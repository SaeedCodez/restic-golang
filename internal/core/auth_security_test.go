package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func authRequest(t *testing.T, h http.Handler, path, remote string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, jsonBody(t, body))
	req.RemoteAddr = remote
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthSetupNeedsTheSetupToken(t *testing.T) {
	app := testApp(t, newResticRunner())
	h := NewServer(app).Routes(http.NotFoundHandler())

	// An app that was given no token refuses every setup over HTTP.
	if rec := authRequest(t, h, "/api/auth/setup", "203.0.113.1:1", map[string]any{"password": "a-long-password"}); rec.Code != http.StatusForbidden {
		t.Fatalf("setup with no token configured: code=%d, want 403", rec.Code)
	}

	app.SetupToken = "0123456789abcdef"
	for _, token := range []string{"", "wrong", "0123456789abcde"} {
		rec := authRequest(t, h, "/api/auth/setup", "203.0.113.2:1", map[string]any{"password": "a-long-password", "setupToken": token})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("setup with token %q: code=%d, want 403", token, rec.Code)
		}
	}
	if app.Auth.Configured() {
		t.Fatal("a refused setup set the password")
	}
	rec := authRequest(t, h, "/api/auth/setup", "203.0.113.3:1", map[string]any{"password": "a-long-password", "setupToken": " 0123456789abcdef "})
	if rec.Code != http.StatusOK || cookieNamed(rec, sessionCookieName) == nil {
		t.Fatalf("setup with the token: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestNewSetupTokenIsRandomUnlessSetInTheEnvironment(t *testing.T) {
	t.Setenv(SetupTokenEnv, "")
	a, fromEnv, err := NewSetupToken()
	if err != nil || fromEnv || len(a) != 2*setupTokenBytes {
		t.Fatalf("token=%q fromEnv=%v err=%v", a, fromEnv, err)
	}
	if b, _, _ := NewSetupToken(); b == a {
		t.Fatal("two random tokens are equal")
	}
	t.Setenv(SetupTokenEnv, " from-env ")
	if tok, fromEnv, _ := NewSetupToken(); tok != "from-env" || !fromEnv {
		t.Fatalf("env token = %q fromEnv=%v", tok, fromEnv)
	}
}

func TestPasswordsNeedTwelveCharacters(t *testing.T) {
	app := testApp(t, newResticRunner())
	if err := app.Auth.SetupPassword("elevenchars"); err == nil {
		t.Fatal("an 11-character password was accepted")
	}
	if err := app.Auth.SetupPassword("twelve-chars"); err != nil {
		t.Fatalf("a 12-character password was refused: %v", err)
	}
}

func TestLoginLocksOutAClientAfterRepeatedWrongPasswords(t *testing.T) {
	app := testApp(t, newResticRunner())
	if err := app.Auth.SetupPassword("the-right-password"); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(app)
	now := time.Now()
	srv.logins.now = func() time.Time { return now }
	h := srv.Routes(http.NotFoundHandler())
	const attacker, owner = "203.0.113.7:4000", "198.51.100.4:5000"

	for i := 0; i < loginMaxFailures; i++ {
		if rec := authRequest(t, h, "/api/auth/login", attacker, map[string]any{"password": "guess"}); rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong password %d: code=%d, want 401", i+1, rec.Code)
		}
	}
	// Locked out: even the right password is refused, before it is checked.
	rec := authRequest(t, h, "/api/auth/login", attacker, map[string]any{"password": "the-right-password"})
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("locked-out login: code=%d retry-after=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
	// Another client is not affected.
	if rec := authRequest(t, h, "/api/auth/login", owner, map[string]any{"password": "the-right-password"}); rec.Code != http.StatusOK {
		t.Fatalf("other client: code=%d, want 200", rec.Code)
	}
	// The lockout ends.
	now = now.Add(loginLockout + time.Second)
	if rec := authRequest(t, h, "/api/auth/login", attacker, map[string]any{"password": "the-right-password"}); rec.Code != http.StatusOK {
		t.Fatalf("after the lockout: code=%d, want 200", rec.Code)
	}
}

func TestLoginLimiterForgetsFailuresAfterSuccessAndWindow(t *testing.T) {
	l := newLoginLimiter()
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < loginMaxFailures-1; i++ {
		l.fail("c")
	}
	l.succeed("c")
	l.fail("c")
	if l.wait("c") != 0 {
		t.Fatal("failures before a success still counted")
	}
	for i := 0; i < loginMaxFailures-2; i++ {
		l.fail("c")
	}
	now = now.Add(loginFailureWindow + time.Second)
	l.fail("c")
	if l.wait("c") != 0 {
		t.Fatal("failures older than the window still counted")
	}
}

func TestLoginClientKey(t *testing.T) {
	cases := []struct {
		remote, xff, want string
	}{
		{"203.0.113.9:1234", "", "203.0.113.9"},
		// A public peer cannot choose its key with X-Forwarded-For.
		{"203.0.113.9:1234", "198.51.100.1", "203.0.113.9"},
		// Behind a proxy, the hop the proxy appended (the last) is the client.
		{"10.0.1.5:1234", "198.51.100.1, 203.0.113.50", "203.0.113.50"},
		{"127.0.0.1:1234", "203.0.113.51", "203.0.113.51"},
		{"10.0.1.5:1234", "not-an-ip", "10.0.1.5"},
		{"[2001:db8:1:2:3:4:5:6]:1234", "", "2001:db8:1:2::/64"},
	}
	for _, c := range cases {
		req := httptest.NewRequest("POST", "/api/auth/login", nil)
		req.RemoteAddr = c.remote
		if c.xff != "" {
			req.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := loginClientKey(req); got != c.want {
			t.Errorf("remote=%s xff=%q: key=%q, want %q", c.remote, c.xff, got, c.want)
		}
	}
}

func TestSecurityHeadersAndSessionCookie(t *testing.T) {
	app := testApp(t, newResticRunner())
	if err := app.Auth.SetupPassword("the-right-password"); err != nil {
		t.Fatal(err)
	}
	h := NewServer(app).Routes(http.NotFoundHandler())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	for name, want := range map[string]string{
		"Content-Security-Policy": contentSecurityPolicy,
		"X-Frame-Options":         "DENY",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "same-origin",
	} {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS over plain HTTP: %q", got)
	}

	// Plain HTTP (a local install): the cookie works without Secure.
	rec = authRequest(t, h, "/api/auth/login", "192.0.2.1:1", map[string]any{"password": "the-right-password"})
	c := cookieNamed(rec, sessionCookieName)
	if c == nil || c.Secure || c.SameSite != http.SameSiteStrictMode || !c.HttpOnly {
		t.Fatalf("http cookie = %+v", c)
	}

	// HTTPS at the proxy: Secure cookie and HSTS.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/auth/login", jsonBody(t, map[string]any{"password": "the-right-password"}))
	req.Header.Set("X-Forwarded-Proto", "https")
	h.ServeHTTP(rec, req)
	c = cookieNamed(rec, sessionCookieName)
	if c == nil || !c.Secure || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("https cookie = %+v", c)
	}
	if !strings.HasPrefix(rec.Header().Get("Strict-Transport-Security"), "max-age=") {
		t.Fatalf("HSTS over https = %q", rec.Header().Get("Strict-Transport-Security"))
	}
}

func TestStateChangingRequestsFromOtherSitesAreRefused(t *testing.T) {
	h := testServer(t)
	post := func(headers map[string]string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "http://backups.example.com/api/folders", jsonBody(t, map[string]any{"name": "f" + headers["Sec-Fetch-Site"] + headers["Origin"], "path": "/data"}))
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	for _, headers := range []map[string]string{
		{"Sec-Fetch-Site": "cross-site"},
		// A sibling subdomain is the same site, but not the same origin.
		{"Sec-Fetch-Site": "same-site"},
		{"Origin": "https://blog.example.com"},
	} {
		if code := post(headers); code != http.StatusForbidden {
			t.Errorf("POST with %v: code=%d, want 403", headers, code)
		}
	}
	for _, headers := range []map[string]string{
		{"Sec-Fetch-Site": "same-origin"},
		{"Origin": "http://backups.example.com"},
		{}, // not a browser
	} {
		if code := post(headers); code != http.StatusCreated && code != http.StatusOK {
			t.Errorf("POST with %v: code=%d, want success", headers, code)
		}
	}

	// Reads stay open to links and bookmarks.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/folders", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cross-site GET: code=%d, want 200", rec.Code)
	}
}
