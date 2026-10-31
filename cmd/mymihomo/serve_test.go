package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testGateway(t *testing.T) *gateway {
	t.Helper()
	t.Setenv("EXTERNAL_SECRET", "private-core-secret")
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("zashboard test shell"), 0600)
	m := newProfileManager(filepath.Join(dir, "config.yaml"))
	_ = m.save(ProfileState{Version: 1, Timezone: "Asia/Hong_Kong", Profiles: []Profile{}})
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-core-secret" || r.Header.Get("Cookie") != "" {
			t.Error("core authentication/cookie isolation failed")
		}
		respondJSON(w, 200, map[string]string{"version": "test"})
	}))
	t.Cleanup(core.Close)
	return newGateway(m, "web-password", dir, core.URL)
}
func loginCookie(t *testing.T, g *gateway) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest("POST", "http://example.test/auth/login", strings.NewReader(url.Values{"password": {"web-password"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if rec.Code != 303 {
		t.Fatalf("login: %d", rec.Code)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("invalid session cookie")
	}
	return cookies[0]
}
func TestGatewayLoginProtectsUIAndAPIs(t *testing.T) {
	g := testGateway(t)
	for _, path := range []string{"/", "/ui/zashboard/", "/ui/zashboard/assets/app.js"} {
		rec := httptest.NewRecorder()
		g.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 303 || rec.Header().Get("Location") != "/login" {
			t.Fatalf("unprotected UI %s", path)
		}
	}
	for _, path := range []string{"/api/profiles", "/configs", "/connections", "/auth/session"} {
		rec := httptest.NewRecorder()
		g.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 401 {
			t.Fatalf("unprotected API %s", path)
		}
	}
	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader("password=wrong"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("wrong password created session")
	}
	cookie := loginCookie(t, g)
	for _, path := range []string{"/ui/zashboard/", "/api/profiles", "/version"} {
		req = httptest.NewRequest("GET", "http://example.test"+path, nil)
		req.AddCookie(cookie)
		rec = httptest.NewRecorder()
		g.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("authenticated %s: %d", path, rec.Code)
		}
	}
	for _, path := range []string{"/ui/yacd/", "/ui/dashboard/"} {
		req = httptest.NewRequest("GET", path, nil)
		req.AddCookie(cookie)
		rec = httptest.NewRecorder()
		g.ServeHTTP(rec, req)
		if rec.Code != 404 {
			t.Fatal("removed panel still available")
		}
	}
	req = httptest.NewRequest("POST", "http://example.test/auth/logout", nil)
	req.AddCookie(cookie)
	req.Header.Set("Origin", "https://evil.test")
	rec = httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal("cross-origin mutation accepted")
	}
	req.Header.Set("Origin", "http://example.test")
	rec = httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal("logout failed")
	}
	req = httptest.NewRequest("GET", "/api/profiles", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatal("logged-out cookie accepted")
	}
}
func TestGatewayBearerAndBlankSecret(t *testing.T) {
	g := testGateway(t)
	req := httptest.NewRequest("GET", "/api/profiles", nil)
	req.Header.Set("Authorization", "Bearer web-password")
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	var body map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != 200 || body["profiles"] == nil {
		t.Fatal("CLI bearer failed")
	}
	g.secret = ""
	rec = httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatal("empty secret bypass")
	}
}

func TestDeploymentPortsCannotBePatched(t *testing.T) {
	g := testGateway(t)
	for _, body := range []string{`{"mixed-port":8000}`, `{"port":8080,"mode":"global"}`, `{"socks-port":1080}`, `{"redir-port":1}`, `{"tproxy-port":1}`, `{"secret":"changed"}`} {
		req := httptest.NewRequest("PATCH", "http://example.test/configs", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer web-password")
		rec := httptest.NewRecorder()
		g.ServeHTTP(rec, req)
		if rec.Code != 409 {
			t.Fatal("deployment setting forwarded to core")
		}
	}
	req := httptest.NewRequest("PATCH", "http://example.test/configs", strings.NewReader(`{"mode":"global"}`))
	req.Header.Set("Authorization", "Bearer web-password")
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal("ordinary runtime setting blocked")
	}
}
