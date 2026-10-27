package core

import (
	"net/http"
	"strings"
)

// contentSecurityPolicy lets the page load only this origin's own scripts,
// styles, fonts and API, and never be framed. Styles allow 'unsafe-inline'
// because UI components (toasts, dialogs) insert <style> elements at runtime;
// scripts do not, so injected markup cannot run code. The build inlines small
// images and fonts as data: URIs.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self' data:; connect-src 'self'; object-src 'none'; " +
	"base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

// securityHeaders sets browser hardening headers on every response: the
// content security policy, no framing (clickjacking), no MIME sniffing, no
// Referer to other sites, and, over HTTPS, HSTS so the browser never falls
// back to plain HTTP for this host.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		if requestIsHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// requestIsHTTPS reports whether the browser reached the app over HTTPS: TLS
// here, or at the reverse proxy in front (Coolify's Traefik), which says so in
// X-Forwarded-Proto. A client that fakes the header only marks its own cookie
// Secure, which then is not sent back over plain HTTP.
func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

// sameOriginOnly refuses state-changing requests (POST, PUT, DELETE, …) that a
// browser sends from another origin, using Sec-Fetch-Site and Origin. That
// covers sites the session cookie's SameSite does not: other apps on sibling
// subdomains of the same domain count as the same site. Requests without
// either header (curl, scripts) are not from a browser and pass.
func sameOriginOnly(next http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		errorJSON(w, http.StatusForbidden, "cross_origin", "Requests from other sites are not allowed.")
	}))
	return cop.Handler(next)
}
