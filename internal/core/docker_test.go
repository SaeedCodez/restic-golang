package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeEngine serves the slice of the Docker Engine API the client uses over a
// unix socket. Exec "echo-stdin" copies its stdin back as stdout; "fail" writes
// to stderr and exits 3.
type fakeEngine struct {
	mu      sync.Mutex
	execs   map[string][]string // exec id -> cmd
	exits   map[string]int
	stopped []string
}

func startFakeEngine(t *testing.T) (*dockerClient, *fakeEngine) {
	t.Helper()
	// macOS caps unix socket paths at 104 bytes, so keep the path short.
	dir, err := os.MkdirTemp("", "dk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	fe := &fakeEngine{execs: map[string][]string{}, exits: map[string]int{}}
	srv := &http.Server{Handler: http.HandlerFunc(fe.serve)}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	t.Setenv("DOCKER_HOST", "unix://"+sock)
	return newDockerClient(), fe
}

func (fe *fakeEngine) serve(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/_ping":
		io.WriteString(w, "OK")
	case p == "/containers/json":
		json.NewEncoder(w).Encode([]map[string]any{{
			"Id": "abc123", "Names": []string{"/wordpress-svc1"}, "Image": "wordpress:6", "State": "running",
			"Labels": map[string]string{"coolify.serviceUuid": "svc1"},
			"Mounts": []map[string]any{{"Type": "volume", "Name": "svc1_files", "Source": "/var/lib/docker/volumes/svc1_files/_data", "Destination": "/var/www/html", "RW": true}},
		}})
	case p == "/containers/abc123/json":
		json.NewEncoder(w).Encode(map[string]any{
			"Id": "abc123", "Name": "/wordpress-svc1",
			"Config": map[string]any{"Image": "wordpress:6", "Hostname": "abc123", "Env": []string{"WORDPRESS_DB_HOST=db1:3306"}, "Labels": map[string]string{"a": "b"}},
			"State":  map[string]any{"Status": "running"},
			"NetworkSettings": map[string]any{"Networks": map[string]any{
				"coolify": map[string]any{"Aliases": []string{"wordpress"}, "DNSNames": []string{"wordpress", "abc123"}},
			}},
		})
	case p == "/containers/missing/json":
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"message":"No such container: missing"}`)
	case strings.HasPrefix(p, "/containers/") && (strings.HasSuffix(p, "/stop") || strings.HasSuffix(p, "/start")):
		fe.mu.Lock()
		fe.stopped = append(fe.stopped, p)
		fe.mu.Unlock()
		w.WriteHeader(http.StatusNotModified)
	case strings.HasPrefix(p, "/containers/") && strings.HasSuffix(p, "/exec"):
		var spec struct{ Cmd []string }
		json.NewDecoder(r.Body).Decode(&spec)
		fe.mu.Lock()
		id := "exec" + string(rune('a'+len(fe.execs)))
		fe.execs[id] = spec.Cmd
		fe.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"Id": id})
	case strings.HasPrefix(p, "/exec/") && strings.HasSuffix(p, "/start"):
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/exec/"), "/start")
		fe.mu.Lock()
		cmd := fe.execs[id]
		fe.mu.Unlock()
		io.Copy(io.Discard, r.Body) // the start options, as the Engine reads them
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		io.WriteString(conn, "HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
		frame := func(stream byte, b []byte) {
			var hdr [8]byte
			hdr[0] = stream
			binary.BigEndian.PutUint32(hdr[4:], uint32(len(b)))
			conn.Write(hdr[:])
			conn.Write(b)
		}
		code := 0
		switch cmd[0] {
		case "echo-stdin":
			in, _ := io.ReadAll(buf) // returns at the client's CloseWrite
			frame(1, []byte("got:"))
			frame(1, in)
		case "fail":
			frame(2, []byte("boom\n"))
			code = 3
		default:
			frame(1, []byte(strings.Join(cmd, " ")))
		}
		fe.mu.Lock()
		fe.exits[id] = code
		fe.mu.Unlock()
	case strings.HasPrefix(p, "/exec/") && strings.HasSuffix(p, "/json"):
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/exec/"), "/json")
		fe.mu.Lock()
		code := fe.exits[id]
		fe.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"Running": false, "ExitCode": code})
	default:
		http.NotFound(w, r)
	}
}

func TestDockerClientListAndInspect(t *testing.T) {
	d, _ := startFakeEngine(t)
	ctx := context.Background()
	if err := d.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	list, err := d.Containers(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("containers: %v %+v", err, list)
	}
	c := list[0]
	if c.Name != "wordpress-svc1" || !c.Running() || c.Labels["coolify.serviceUuid"] != "svc1" {
		t.Fatalf("container = %+v", c)
	}
	if len(c.Mounts) != 1 || c.Mounts[0].Name != "svc1_files" || c.Mounts[0].Destination != "/var/www/html" {
		t.Fatalf("mounts = %+v", c.Mounts)
	}

	full, err := d.Inspect(ctx, "abc123")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if envValue(full.Env, "WORDPRESS_DB_HOST") != "db1:3306" || full.Hostname != "abc123" {
		t.Fatalf("inspect = %+v", full)
	}
	if strings.Join(full.Aliases, ",") != "wordpress,abc123" {
		t.Fatalf("aliases = %v", full.Aliases)
	}

	_, err = d.Inspect(ctx, "missing")
	if !isDockerNotFound(err) || !strings.Contains(err.Error(), "No such container") {
		t.Fatalf("missing inspect err = %v", err)
	}
	if err := d.Stop(ctx, "abc123"); err != nil {
		t.Fatalf("stop (304 counts as done): %v", err)
	}
}

func TestDockerClientExecStreamsStdinAndOutput(t *testing.T) {
	d, _ := startFakeEngine(t)
	ctx := context.Background()

	var out, errOut bytes.Buffer
	code, err := d.Exec(ctx, "abc123", []string{"echo-stdin"}, strings.NewReader("hello dump"), &out, &errOut)
	if err != nil || code != 0 {
		t.Fatalf("exec: code=%d err=%v", code, err)
	}
	if out.String() != "got:hello dump" {
		t.Fatalf("stdout = %q", out.String())
	}

	out.Reset()
	code, err = d.Exec(ctx, "abc123", []string{"fail"}, nil, &out, &errOut)
	if err != nil || code != 3 || errOut.String() != "boom\n" {
		t.Fatalf("failing exec: code=%d err=%v stderr=%q", code, err, errOut.String())
	}
}

func TestDockerClientExecReportsBrokenInput(t *testing.T) {
	d, _ := startFakeEngine(t)
	src := io.MultiReader(strings.NewReader("partial"), errReader{errors.New("restic died")})
	_, err := d.Exec(context.Background(), "abc123", []string{"echo-stdin"}, src, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "restic died") {
		t.Fatalf("a failed stdin source must fail the exec, got %v", err)
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func TestDockerClientUnreachableSocket(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///nonexistent/docker.sock")
	d := newDockerClient()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := d.Ping(ctx)
	if err == nil || !strings.Contains(err.Error(), "is not available") {
		t.Fatalf("ping err = %v", err)
	}
}

func TestDemuxDockerStream(t *testing.T) {
	var raw bytes.Buffer
	w := bufio.NewWriter(&raw)
	for _, f := range []struct {
		stream byte
		data   string
	}{{1, "out1 "}, {2, "err1"}, {1, "out2"}} {
		var hdr [8]byte
		hdr[0] = f.stream
		binary.BigEndian.PutUint32(hdr[4:], uint32(len(f.data)))
		w.Write(hdr[:])
		w.WriteString(f.data)
	}
	w.Flush()
	var out, errOut bytes.Buffer
	if err := demuxDockerStream(&raw, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if out.String() != "out1 out2" || errOut.String() != "err1" {
		t.Fatalf("out=%q err=%q", out.String(), errOut.String())
	}
	// A frame cut short is an error, not a silent truncation.
	short := []byte{1, 0, 0, 0, 0, 0, 0, 9, 'a', 'b'}
	if err := demuxDockerStream(bytes.NewReader(short), io.Discard, io.Discard); err == nil {
		t.Fatal("expected an error for a truncated frame")
	}
}

func TestContainerIDFromMountinfo(t *testing.T) {
	id := strings.Repeat("ab12", 16)
	other := strings.Repeat("cd34", 16)
	info := "1 2 0:1 / / rw - overlay overlay rw\n" +
		// Another container's files exposed through a bind mount: not ours.
		"2 2 254:1 /var/lib/docker/containers/" + other + "/logs /mnt/logs rw - ext4 /dev/vda1 rw\n" +
		"3 2 254:1 /var/lib/docker/containers/" + id + "/hostname /etc/hostname rw - ext4 /dev/vda1 rw\n"
	if got := containerIDFromMountinfo(info); got != id {
		t.Fatalf("got %q", got)
	}
	if got := containerIDFromMountinfo("1 2 0:1 / / rw - overlay overlay rw\n"); got != "" {
		t.Fatalf("expected none, got %q", got)
	}
}
