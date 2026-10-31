package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func passwordRequest(g *gateway, cookie *http.Cookie, current, next, confirm string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"current_password": current, "new_password": next, "confirm_password": confirm})
	req := httptest.NewRequest("POST", "http://example.test/auth/password", strings.NewReader(string(body)))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	return rec
}
func TestPasswordChangePersistsAndRevokesSessions(t *testing.T) {
	g := testGateway(t)
	if err := g.loadAuthentication(); err != nil {
		t.Fatal(err)
	}
	cookie := loginCookie(t, g)
	for _, args := range [][3]string{{"wrong", "new-password-123", "new-password-123"}, {"web-password", "new-password-123", "different"}, {"web-password", "short", "short"}} {
		if passwordRequest(g, cookie, args[0], args[1], args[2]).Code != 400 {
			t.Fatal("invalid change accepted")
		}
	}
	if passwordRequest(g, cookie, "web-password", "new-password-123", "new-password-123").Code != 200 {
		t.Fatal("change failed")
	}
	req := httptest.NewRequest("GET", "/api/profiles", nil)
	req.AddCookie(cookie)
	if g.authorized(req) {
		t.Fatal("old session remains valid")
	}
	req = httptest.NewRequest("GET", "/api/profiles", nil)
	req.Header.Set("Authorization", "Bearer web-password")
	if g.authorized(req) {
		t.Fatal("old environment credential bypass")
	}
	req.Header.Set("Authorization", "Bearer new-password-123")
	if !g.authorized(req) {
		t.Fatal("new password rejected")
	}
	data, _ := os.ReadFile(authPath(g.manager.confFile))
	if strings.Contains(string(data), "new-password-123") {
		t.Fatal("plaintext password stored")
	}
	info, _ := os.Stat(authPath(g.manager.confFile))
	if info.Mode().Perm() != 0600 {
		t.Fatal("password file permissions")
	}
	fresh := newGateway(g.manager, "web-password", ".", "http://127.0.0.1:1")
	if err := fresh.loadAuthentication(); err != nil {
		t.Fatal(err)
	}
	if !fresh.authorized(req) {
		t.Fatal("password lost after restart")
	}
	req = httptest.NewRequest("POST", "/api/profiles/update-active", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Mymihomo-CLI-Token", fresh.cliToken)
	if !fresh.authorized(req) {
		t.Fatal("local CLI broken after rotation")
	}
	req.RemoteAddr = "192.168.0.10:12345"
	if fresh.authorized(req) {
		t.Fatal("CLI token accepted remotely")
	}
}
func TestPasswordWriteFailurePreservesOldAccess(t *testing.T) {
	g := testGateway(t)
	cookie := loginCookie(t, g)
	if err := os.Mkdir(authPath(g.manager.confFile), 0700); err != nil {
		t.Fatal(err)
	}
	if passwordRequest(g, cookie, "web-password", "new-password-123", "new-password-123").Code != 500 {
		t.Fatal("failed persistence not reported")
	}
	if g.passwordHash != "" {
		t.Fatal("failed write replaced password")
	}
	req := httptest.NewRequest("GET", "/api/profiles", nil)
	req.AddCookie(cookie)
	if !g.authorized(req) {
		t.Fatal("failed change revoked session")
	}
}
