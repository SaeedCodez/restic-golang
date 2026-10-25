package core

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// This file holds the restic operations that move data through a pipe instead
// of the filesystem: backing up a database dump from stdin, streaming a dump
// back out of a snapshot, and listing a snapshot's files for the browser.

// writeErrRecorder remembers the first error writing to restic's stdin, so a
// producer that failed because restic went away is told apart from one that
// failed on its own.
type writeErrRecorder struct {
	w   io.Writer
	mu  sync.Mutex
	err error
}

func (r *writeErrRecorder) Write(p []byte) (int, error) {
	n, err := r.w.Write(p)
	if err != nil {
		r.mu.Lock()
		if r.err == nil {
			r.err = err
		}
		r.mu.Unlock()
	}
	return n, err
}

func (r *writeErrRecorder) failed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err != nil
}

func (*resticRunner) BackupStdin(ctx context.Context, repo *Repository, filename string, tags []string, produce func(w io.Writer) error, sink RunSink) (int, error) {
	args := []string{"backup", "--stdin", "--stdin-filename", filename, "--json"}
	for _, tg := range tags {
		if strings.TrimSpace(tg) != "" {
			args = append(args, "--tag", tg)
		}
	}
	// Canceling rctx sends restic SIGINT (resticCommand's Cancel) and kills it
	// after the grace period. Its stdin is never closed early, so it cannot
	// mistake a failed dump for a complete one.
	rctx, abort := context.WithCancel(ctx)
	defer abort()
	cmd, err := resticCommand(rctx, repo, args...)
	if err != nil {
		return -1, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return -1, err
	}
	// If this process dies, restic must die too: otherwise it would see the end
	// of its input and save the truncated dump as a snapshot.
	killWithParent(cmd)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	in := &writeErrRecorder{w: stdin}
	prodErr := make(chan error, 1)
	var aborted atomic.Bool
	// Once stopped (SIGINT), restic releases its lock but stays blocked reading
	// the stdin it is never given the end of; end it shortly after rather than
	// waiting out the full grace period. It never saves a snapshot either way.
	exited := make(chan struct{})
	defer close(exited)
	stopKill := context.AfterFunc(rctx, func() {
		select {
		case <-exited:
		case <-time.After(stdinKillAfter):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}
	})
	defer stopKill()
	code, err := streamResticCmd(cmd, KindBackup, sink, func() {
		go func() {
			if err := produce(in); err != nil && !in.failed() {
				aborted.Store(true)
				abort()
				prodErr <- err
				return
			}
			prodErr <- stdin.Close()
		}()
	})
	if err != nil {
		return code, err
	}
	perr := <-prodErr
	switch {
	case ctx.Err() != nil:
		return resticExitInterrupted, nil
	case aborted.Load():
		// The dump failed and restic was stopped before it saw the end of input.
		return code, perr
	case code != 0:
		// restic quit on its own (not initialized, locked, …) while the dump
		// was still going in; writing or closing its stdin then fails, but the
		// exit code is what explains it.
		return code, nil
	default:
		return code, perr
	}
}

// stdinKillAfter is how long a stopped `restic backup --stdin` gets before it
// is killed.
const stdinKillAfter = 2 * time.Second

func (*resticRunner) Dump(ctx context.Context, repo *Repository, snapshotID, file string, consume func(r io.Reader) error, sink RunSink) (int, error) {
	rctx, abort := context.WithCancel(ctx)
	defer abort()
	cmd, err := resticCommand(rctx, repo, "dump", snapshotID, file)
	if err != nil {
		return -1, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return -1, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return -1, err
	}
	if err := cmd.Start(); err != nil {
		return -1, fmt.Errorf("could not start restic: %w", err)
	}
	if cmd.Process != nil {
		sink.PID(cmd.Process.Pid, procStartToken(cmd.Process.Pid))
	}

	var (
		wg      sync.WaitGroup
		diagMu  sync.Mutex
		diagBuf strings.Builder
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			diagMu.Lock()
			if diagBuf.Len() < 8*1024 {
				diagBuf.WriteString(line + "\n")
			}
			diagMu.Unlock()
			sink.Log("warn", "stderr", clipLogLine(line))
		}
		_, _ = io.Copy(io.Discard, stderr)
	}()

	consumeErr := consume(stdout)
	if consumeErr != nil {
		abort()
	}
	// Drain what is left so restic can exit, then collect its verdict.
	_, _ = io.Copy(io.Discard, stdout)
	wg.Wait()
	code := exitCode(cmd.Wait())
	if consumeErr != nil {
		if ctx.Err() != nil {
			return resticExitInterrupted, nil
		}
		return code, consumeErr
	}
	if code != 0 && code != resticExitInterrupted && code != resticExitNotInitialized &&
		code != resticExitLocked && code != resticExitBadPassword {
		diagMu.Lock()
		text := diagBuf.String()
		diagMu.Unlock()
		if mapped := classifyResticFailure(text); mapped != 0 {
			code = mapped
		}
	}
	return code, nil
}

func (*resticRunner) List(ctx context.Context, repo *Repository, snapshotID string) ([]LsNode, error) {
	cmd, err := resticCommand(ctx, repo, "ls", "--json", snapshotID)
	if err != nil {
		return nil, err
	}
	out, stderr, err := outputSeparately(ctx, cmd)
	if err != nil {
		return nil, outputError(ctx, "restic ls", out, stderr, err)
	}
	nodes, err := decodeLs(out)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 && len(strings.TrimSpace(string(out))) == 0 {
		return nil, errors.New("restic ls returned no output")
	}
	return nodes, nil
}
