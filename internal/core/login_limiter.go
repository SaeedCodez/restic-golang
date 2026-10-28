package core

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Logging in to this app is root on the server (it holds the Docker socket), so
// password guessing is slowed: after loginMaxFailures wrong passwords (or setup
// tokens) from one client within loginFailureWindow, that client is refused for
// loginLockout. A refused attempt is answered before the password is hashed, so
// it costs the server nothing.
const (
	loginMaxFailures   = 5
	loginFailureWindow = 15 * time.Minute
	loginLockout       = 15 * time.Minute
	// loginClientsPruneAt bounds the table: past it, settled entries are dropped.
	loginClientsPruneAt = 10_000
)

type loginLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	clients map[string]*loginFailures
}

type loginFailures struct {
	count       int
	windowStart time.Time
	lockedUntil time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{now: time.Now, clients: make(map[string]*loginFailures)}
}

// wait reports how long the client must wait before trying again (0: it may).
func (l *loginLimiter) wait(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	c, ok := l.clients[key]
	if !ok {
		return 0
	}
	if left := c.lockedUntil.Sub(l.now()); left > 0 {
		return left
	}
	return 0
}

// fail records a wrong attempt and locks the client out once it has too many.
func (l *loginLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.clients) >= loginClientsPruneAt {
		l.pruneLocked(now)
	}
	c, ok := l.clients[key]
	if !ok {
		c = &loginFailures{}
		l.clients[key] = c
	}
	if now.Sub(c.windowStart) > loginFailureWindow {
		c.count, c.windowStart = 0, now
	}
	c.count++
	if c.count >= loginMaxFailures {
		c.lockedUntil = now.Add(loginLockout)
		c.count, c.windowStart = 0, now
	}
}

// succeed forgets the client's earlier wrong attempts.
func (l *loginLimiter) succeed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.clients, key)
}

func (l *loginLimiter) pruneLocked(now time.Time) {
	for key, c := range l.clients {
		if now.After(c.lockedUntil) && now.Sub(c.windowStart) > loginFailureWindow {
			delete(l.clients, key)
		}
	}
}

// loginClientKey names the client an attempt comes from. Behind a reverse proxy
// (Coolify's Traefik) every request arrives from the proxy, so when the direct
// peer is a loopback or private address, the last X-Forwarded-For entry, the one
// that proxy appended, names the client instead. A public peer is taken as is,
// since it could write any X-Forwarded-For it liked. IPv6 clients are grouped by
// their /64, which one client usually holds whole.
func loginClientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		if hops := r.Header.Values("X-Forwarded-For"); len(hops) > 0 {
			list := strings.Split(hops[len(hops)-1], ",")
			if fwd := net.ParseIP(strings.TrimSpace(list[len(list)-1])); fwd != nil {
				ip = fwd
			}
		}
	}
	if ip == nil {
		return host
	}
	if ip.To4() == nil {
		return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
	}
	return ip.String()
}
