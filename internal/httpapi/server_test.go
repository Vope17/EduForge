package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"eduforge/internal/app"
	"eduforge/internal/store"
)

func handler(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	s, err := store.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	a, err := app.New(s, filepath.Join(root, "secrets"))
	if err != nil {
		t.Fatal(err)
	}
	return New(a, filepath.Join(root, "web"), []string{"http://localhost:8080"})
}
func TestHostOriginAndWriteToken(t *testing.T) {
	s := handler(t)
	cases := []struct {
		host, origin, token string
		want                int
	}{{"attacker.invalid", "", "", 403}, {"localhost:8080", "https://attacker.invalid", s.Token, 403}, {"localhost:8080", "http://localhost:8080", "", 403}, {"localhost:8080", "http://localhost:8080", s.Token, 200}}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/api/courses", strings.NewReader(`{"name":"Research","description":"","style":{}}`))
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("X-EduForge-Token", tc.token)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("got %d want %d: %s", rec.Code, tc.want, rec.Body.String())
		}
	}
}
func TestReadOnlyRefusesWritesButKeepsReading(t *testing.T) {
	s := handler(t)
	s.ReadOnly = true
	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://localhost:8080"+path, strings.NewReader(body))
		req.Header.Set("X-EduForge-Token", s.Token)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}
	for _, w := range []struct{ method, path string }{{"POST", "/api/courses"}, {"PUT", "/api/preferences"}, {"DELETE", "/api/records/x"}, {"POST", "/api/providers/x/login"}} {
		if rec := do(w.method, w.path, `{}`); rec.Code != 403 || !strings.Contains(rec.Body.String(), "read_only") {
			t.Fatalf("%s %s: %d %s", w.method, w.path, rec.Code, rec.Body.String())
		}
	}
	if rec := do("GET", "/api/courses", ""); rec.Code != 200 {
		t.Fatalf("read refused: %d", rec.Code)
	}
	if rec := do("GET", "/api/session", ""); !strings.Contains(rec.Body.String(), `"read_only":true`) {
		t.Fatalf("session: %s", rec.Body.String())
	}
	if rec := do("POST", "/api/math", `{"formulas":[]}`); rec.Code != 200 {
		t.Fatalf("math refused: %d %s", rec.Code, rec.Body.String())
	}
}
func TestClientCannotAssignAnOwner(t *testing.T) {
	s := handler(t)
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/courses", strings.NewReader(`{"name":"Research","owner_user_id":"someone-else"}`))
	req.Header.Set("X-EduForge-Token", s.Token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("client ownership accepted: %d", rec.Code)
	}
}

func TestFramingIsDeniedUnlessAncestorsAreConfigured(t *testing.T) {
	s := handler(t)
	get := func() http.Header {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://localhost:8080/api/health", nil))
		return rec.Header()
	}
	if h := get(); h.Get("X-Frame-Options") != "DENY" || h.Get("Content-Security-Policy") != "frame-ancestors 'none'" {
		t.Fatalf("default framing headers: %v", h)
	}
	s.FrameAncestors = "http://127.0.0.1:8090"
	if h := get(); h.Get("X-Frame-Options") != "" || h.Get("Content-Security-Policy") != "frame-ancestors http://127.0.0.1:8090" {
		t.Fatalf("portal framing headers: %v", h)
	}
}

func TestPreviewFramesAllowThePortalAncestor(t *testing.T) {
	s := handler(t)
	h := http.Header{}
	s.previewHeaders(h, "sandbox; default-src 'none'")
	if h.Get("X-Frame-Options") != "SAMEORIGIN" || h.Get("Content-Security-Policy") != "sandbox; default-src 'none'; frame-ancestors 'self'" {
		t.Fatalf("default preview headers: %v", h)
	}
	s.FrameAncestors = "http://127.0.0.1:8090 http://localhost:8090"
	h = http.Header{"X-Frame-Options": {"DENY"}}
	s.previewHeaders(h, "sandbox; default-src 'none'")
	if h.Get("X-Frame-Options") != "" ||
		h.Get("Content-Security-Policy") != "sandbox; default-src 'none'; frame-ancestors 'self' http://127.0.0.1:8090 http://localhost:8090" {
		t.Fatalf("portal preview headers: %v", h)
	}
}

func TestFrameAncestorsKeepsOnlyPlainOrigins(t *testing.T) {
	t.Setenv("EDUFORGE_FRAME_ANCESTORS", "http://127.0.0.1:8090  https://portal.example/ 'self' * http://a;b ftp://x http://x/path http://u@x javascript:alert(1)")
	if got := FrameAncestors(); got != "http://127.0.0.1:8090 https://portal.example" {
		t.Fatalf("got %q", got)
	}
}
