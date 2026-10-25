package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Docker is everything the app asks of the Docker Engine: list and inspect
// containers to discover sites and databases (Coolify labels every container it
// runs), run dump and import commands inside a database container, and stop or
// start a site around a restore. All Docker access goes through this interface,
// so tests use a fake and need no daemon.
type Docker interface {
	// Ping reports whether the Engine answers; nil means it is usable.
	Ping(ctx context.Context) error
	// Containers lists every container, running or not. Env and network aliases
	// are only filled by Inspect.
	Containers(ctx context.Context) ([]Container, error)
	// Inspect returns one container in full, by id or name.
	Inspect(ctx context.Context, id string) (Container, error)
	// Exec runs cmd inside a running container and returns its exit code. stdin,
	// if non-nil, is streamed to the command and closed at EOF. err is set only
	// if the command could not be run or its output could not be delivered.
	Exec(ctx context.Context, id string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) (int, error)
	// Stop stops a container (no-op if it is already stopped).
	Stop(ctx context.Context, id string) error
	// Start starts a container (no-op if it is already running).
	Start(ctx context.Context, id string) error
}

// Container is the part of a Docker container the app uses.
type Container struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Image  string            `json:"image"`
	State  string            `json:"state"` // running, exited, …
	Labels map[string]string `json:"-"`
	Mounts []Mount           `json:"-"`
	// Env is only set by Inspect. It holds secrets, so it never leaves the server.
	Env []string `json:"-"`
	// Aliases are the DNS names other containers reach this one by (Inspect only).
	Aliases []string `json:"-"`
	// Hostname is the container's configured hostname (Inspect only).
	Hostname string `json:"-"`
}

// Running reports whether the container is up.
func (c Container) Running() bool { return c.State == "running" }

// Mount is one volume or bind mount of a container.
type Mount struct {
	Type        string `json:"type"`           // volume | bind | tmpfs | …
	Name        string `json:"name,omitempty"` // volume name
	Source      string `json:"source"`         // path on the Docker host
	Destination string `json:"destination"`    // path inside the container
	RW          bool   `json:"rw"`
}

// envValue returns the value of key in a KEY=VALUE environment list.
func envValue(env []string, key string) string {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return kv[len(prefix):]
		}
	}
	return ""
}

// ---- Engine API client -------------------------------------------------------

// defaultDockerSocket is where the Engine listens unless DOCKER_HOST says
// otherwise. On Coolify the app container gets it as a bind mount.
const defaultDockerSocket = "/var/run/docker.sock"

// dockerClient talks to the Engine API over its unix socket (or tcp://) with the
// standard library only, so the image needs no docker CLI.
type dockerClient struct {
	network, address string
	http             *http.Client
}

// newDockerClient builds a client from DOCKER_HOST (unix:// or tcp://), falling
// back to the default socket.
func newDockerClient() *dockerClient {
	network, address := "unix", defaultDockerSocket
	if h := strings.TrimSpace(os.Getenv("DOCKER_HOST")); h != "" {
		if rest, ok := strings.CutPrefix(h, "unix://"); ok {
			address = rest
		} else if rest, ok := strings.CutPrefix(h, "tcp://"); ok {
			network, address = "tcp", rest
		}
	}
	d := &dockerClient{network: network, address: address}
	d.http = &http.Client{Transport: &http.Transport{
		DialContext:     func(ctx context.Context, _, _ string) (net.Conn, error) { return d.dial(ctx) },
		MaxIdleConns:    4,
		IdleConnTimeout: 30 * time.Second,
	}}
	return d
}

// DockerHost names the Engine endpoint, for status messages.
func (d *dockerClient) DockerHost() string { return d.network + "://" + d.address }

func (d *dockerClient) dial(ctx context.Context) (net.Conn, error) {
	var dl net.Dialer
	return dl.DialContext(ctx, d.network, d.address)
}

// dockerAPIError is an error response from the Engine.
type dockerAPIError struct {
	Status  int
	Message string
}

func (e *dockerAPIError) Error() string {
	return fmt.Sprintf("docker: %s (HTTP %d)", e.Message, e.Status)
}

func isDockerNotFound(err error) bool {
	var ae *dockerAPIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}

// do sends one API request. body, if non-nil, is sent as JSON; out, if non-nil,
// receives the JSON response. 304 (already started/stopped) counts as success.
func (d *dockerClient) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return d.unreachable(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return nil
	}
	if resp.StatusCode >= 300 {
		return readDockerError(resp)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func readDockerError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	var m struct {
		Message string `json:"message"`
	}
	msg := strings.TrimSpace(string(b))
	if json.Unmarshal(b, &m) == nil && m.Message != "" {
		msg = m.Message
	}
	if msg == "" {
		msg = resp.Status
	}
	return &dockerAPIError{Status: resp.StatusCode, Message: msg}
}

// unreachable explains a failed connection to the Engine.
func (d *dockerClient) unreachable(err error) error {
	if d.network == "unix" {
		if _, statErr := os.Stat(d.address); errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("the Docker socket %s is not available; mount it into this container to discover sites and back up databases", d.address)
		}
	}
	return fmt.Errorf("could not reach Docker at %s: %w", d.DockerHost(), err)
}

func (d *dockerClient) Ping(ctx context.Context) error {
	return d.do(ctx, http.MethodGet, "/_ping", nil, nil)
}

// apiContainer mirrors one entry of GET /containers/json.
type apiContainer struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Labels map[string]string `json:"Labels"`
	Mounts []apiMount        `json:"Mounts"`
}

type apiMount struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	RW          bool   `json:"RW"`
}

func (m apiMount) mount() Mount {
	return Mount{Type: m.Type, Name: m.Name, Source: m.Source, Destination: m.Destination, RW: m.RW}
}

func (d *dockerClient) Containers(ctx context.Context) ([]Container, error) {
	var list []apiContainer
	if err := d.do(ctx, http.MethodGet, "/containers/json?all=1", nil, &list); err != nil {
		return nil, err
	}
	out := make([]Container, 0, len(list))
	for _, c := range list {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		ct := Container{ID: c.ID, Name: name, Image: c.Image, State: c.State, Labels: c.Labels}
		for _, m := range c.Mounts {
			ct.Mounts = append(ct.Mounts, m.mount())
		}
		out = append(out, ct)
	}
	return out, nil
}

// apiInspect mirrors the parts of GET /containers/{id}/json the app uses.
type apiInspect struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Config struct {
		Image    string            `json:"Image"`
		Hostname string            `json:"Hostname"`
		Env      []string          `json:"Env"`
		Labels   map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Status string `json:"Status"`
	} `json:"State"`
	Mounts          []apiMount `json:"Mounts"`
	NetworkSettings struct {
		Networks map[string]struct {
			Aliases  []string `json:"Aliases"`
			DNSNames []string `json:"DNSNames"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

func (d *dockerClient) Inspect(ctx context.Context, id string) (Container, error) {
	var in apiInspect
	if err := d.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/json", nil, &in); err != nil {
		return Container{}, err
	}
	c := Container{
		ID:       in.ID,
		Name:     strings.TrimPrefix(in.Name, "/"),
		Image:    in.Config.Image,
		State:    in.State.Status,
		Labels:   in.Config.Labels,
		Env:      in.Config.Env,
		Hostname: in.Config.Hostname,
	}
	for _, m := range in.Mounts {
		c.Mounts = append(c.Mounts, m.mount())
	}
	seen := map[string]bool{}
	for _, n := range in.NetworkSettings.Networks {
		for _, a := range append(append([]string(nil), n.Aliases...), n.DNSNames...) {
			if a != "" && !seen[a] {
				seen[a] = true
				c.Aliases = append(c.Aliases, a)
			}
		}
	}
	return c, nil
}

func (d *dockerClient) Stop(ctx context.Context, id string) error {
	return d.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/stop?t=30", nil, nil)
}

func (d *dockerClient) Start(ctx context.Context, id string) error {
	return d.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/start", nil, nil)
}

// Exec creates an exec instance, then starts it on a hijacked connection: the
// Engine upgrades the HTTP request to a raw stream that carries stdin one way
// and multiplexed stdout/stderr frames the other. Half-closing the connection's
// write side is what tells the command its stdin has ended.
func (d *dockerClient) Exec(ctx context.Context, id string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	var created struct {
		ID string `json:"Id"`
	}
	spec := map[string]any{
		"AttachStdin":  stdin != nil,
		"AttachStdout": true,
		"AttachStderr": true,
		"Tty":          false,
		"Cmd":          cmd,
	}
	if err := d.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/exec", spec, &created); err != nil {
		return -1, err
	}

	conn, err := d.dial(ctx)
	if err != nil {
		return -1, d.unreachable(err)
	}
	var closeOnce sync.Once
	closeConn := func() { closeOnce.Do(func() { conn.Close() }) }
	defer closeConn()
	stopWatch := context.AfterFunc(ctx, closeConn)
	defer stopWatch()

	req, err := http.NewRequest(http.MethodPost, "http://docker/exec/"+created.ID+"/start",
		strings.NewReader(`{"Detach":false,"Tty":false}`))
	if err != nil {
		return -1, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "tcp")
	if err := req.Write(conn); err != nil {
		return -1, ctxOr(ctx, fmt.Errorf("docker exec: %w", err))
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		return -1, ctxOr(ctx, fmt.Errorf("docker exec: %w", err))
	}
	if resp.StatusCode != http.StatusSwitchingProtocols && resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return -1, readDockerError(resp)
	}

	// Only a failure to read stdin matters: a write failing because the command
	// already exited is reported by its exit code.
	stdinDone := make(chan error, 1)
	if stdin != nil {
		src := &readErrRecorder{r: stdin}
		go func() {
			_, _ = io.Copy(conn, src)
			if cw, ok := conn.(interface{ CloseWrite() error }); ok {
				_ = cw.CloseWrite()
			}
			stdinDone <- src.err
		}()
	} else {
		stdinDone <- nil
	}

	outErr := demuxDockerStream(br, stdout, stderr)
	// The command has finished writing (or its output can no longer be
	// delivered); closing unblocks a stdin copy still waiting on the peer.
	closeConn()
	var inErr error
	select {
	case inErr = <-stdinDone:
	case <-time.After(5 * time.Second):
		// Still blocked reading the source: the command ended without the rest
		// of its input. The caller closes the source, which ends the copy.
		inErr = errors.New("the command exited before reading all of its input")
	}
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	if outErr != nil {
		return -1, fmt.Errorf("docker exec output: %w", outErr)
	}
	code, err := d.execExitCode(ctx, created.ID)
	if err != nil {
		return -1, err
	}
	// A command that failed says why on stderr; one that succeeded on partial
	// input must not count as a success.
	if inErr != nil && code == 0 {
		return -1, fmt.Errorf("docker exec input: %w", inErr)
	}
	return code, nil
}

// readErrRecorder remembers the first non-EOF error of the reader it wraps.
type readErrRecorder struct {
	r   io.Reader
	err error
}

func (e *readErrRecorder) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil && err != io.EOF && e.err == nil {
		e.err = err
	}
	return n, err
}

// execExitCode waits briefly for the exec to be reported as finished (the
// stream can end a moment before the Engine records the exit) and returns its
// exit code.
func (d *dockerClient) execExitCode(ctx context.Context, execID string) (int, error) {
	for i := 0; ; i++ {
		var st struct {
			Running  bool `json:"Running"`
			ExitCode int  `json:"ExitCode"`
		}
		if err := d.do(ctx, http.MethodGet, "/exec/"+execID+"/json", nil, &st); err != nil {
			return -1, err
		}
		if !st.Running {
			return st.ExitCode, nil
		}
		if i >= 50 {
			return -1, errors.New("docker exec: the command did not report an exit code")
		}
		select {
		case <-ctx.Done():
			return -1, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func ctxOr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// demuxDockerStream splits the Engine's multiplexed exec stream: each frame is
// an 8-byte header (stream id, three zero bytes, big-endian payload length)
// followed by the payload. Stream 1 is stdout, 2 is stderr. A clean EOF between
// frames ends the stream.
func demuxDockerStream(r io.Reader, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	var hdr [8]byte
	for {
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		n := int64(binary.BigEndian.Uint32(hdr[4:]))
		w := io.Discard
		switch hdr[0] {
		case 1:
			w = stdout
		case 2:
			w = stderr
		}
		if _, err := io.CopyN(w, r, n); err != nil {
			return err
		}
	}
}

// ---- this app's own container --------------------------------------------------

// selfContainerID guesses this process's own container id: Docker sets the
// hostname to the short id unless it is overridden, and the container's
// hostname/resolv.conf bind mounts name the full id in mountinfo. "" when the
// app is not running in a container (or the guess fails).
func selfContainerID() string {
	if b, err := os.ReadFile("/proc/self/mountinfo"); err == nil {
		if id := containerIDFromMountinfo(string(b)); id != "" {
			return id
		}
	}
	if h, err := os.Hostname(); err == nil && isHexID(h) {
		return h
	}
	return ""
}

// containerIDFromMountinfo reads the container id from the mount Docker makes
// for /etc/hostname, whose source is .../containers/<64-hex id>/hostname.
// Only that line is used: other mounts may expose other containers' files.
func containerIDFromMountinfo(info string) string {
	const marker = "/containers/"
	for _, line := range strings.Split(info, "\n") {
		f := strings.Fields(line)
		// Fields: id parent major:minor root mount-point …
		if len(f) < 5 || f[4] != "/etc/hostname" {
			continue
		}
		root := f[3]
		i := strings.LastIndex(root, marker)
		if i < 0 {
			continue
		}
		rest := root[i+len(marker):]
		if len(rest) >= 64 && isHexID(rest[:64]) {
			return rest[:64]
		}
	}
	return ""
}

func isHexID(s string) bool {
	if len(s) < 12 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
