package main

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtensionsOrderAndScriptSandbox(t *testing.T) {
	cfg, _ := parseConfig("rules: ['MATCH,DIRECT']\ndns:\n  enable: true\n  nameserver: ['1.1.1.1']\n")
	result, err := applyExtensions(cfg, "prepend-rules:\n  - DOMAIN,example.test,REJECT\ndns:\n  nameserver: ['8.8.8.8']\n", "function main(config, name) { config['profile-name'] = name; return config; }", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if result["rules"].([]interface{})[0] != "DOMAIN,example.test,REJECT" || result["profile-name"] != "demo" {
		t.Fatal("rule precedence/script transform broken")
	}
	if result["dns"].(map[string]interface{})["enable"] != true {
		t.Fatal("deep merge discarded nested settings")
	}
	for _, script := range []string{"function main(c) { return require('fs').readFileSync('/etc/passwd'); }", "function main(c) {}", "function main(c) { return []; }"} {
		if _, err := applyExtensions(cfg, "", script, "test"); err == nil {
			t.Fatal("invalid/unsafe script accepted")
		}
	}
	started := time.Now()
	if _, err := applyExtensions(cfg, "", "function main(c) { while(true) {} }", "test"); err == nil || time.Since(started) > 4*time.Second {
		t.Fatal("script time limit failed")
	}
}
func managerFixture(t *testing.T) (*profileManager, ProfileState) {
	t.Helper()
	t.Setenv("EXTERNAL_PORT", "19097")
	t.Setenv("MIXED_PORT", "7897")
	t.Setenv("EXTERNAL_SECRET", "inner")
	dir := t.TempDir()
	m := newProfileManager(filepath.Join(dir, "config.yaml"))
	m.validate = func([]byte) error { return nil }
	m.apply = func([]byte) error { return nil }
	p := Profile{ID: "one", Name: "One", Content: "proxies: []\nproxy-groups: []\nrules: ['MATCH,DIRECT']\n", Timeout: 1, Retries: 0, UserAgent: "test", UpdatedAt: time.Now(), AttemptedAt: time.Now()}
	s := ProfileState{Version: 1, ActiveID: p.ID, Timezone: "Asia/Hong_Kong", Profiles: []Profile{p}}
	_ = m.save(s)
	_ = os.WriteFile(m.confFile, []byte(p.Content), 0600)
	return m, s
}
func TestFailedUpdateKeepsActiveConfigAndProfile(t *testing.T) {
	m, s := managerFixture(t)
	original, _ := os.ReadFile(m.confFile)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "rules: ['MATCH,REJECT']\nproxies: []\n") }))
	defer upstream.Close()
	s.Profiles[0].URL = upstream.URL
	_ = m.save(s)
	m.apply = func([]byte) error { return fmt.Errorf("core rejected") }
	if err := m.updateLocked("one", nil); err == nil {
		t.Fatal("expected failure")
	}
	current, _ := m.read()
	active, _ := os.ReadFile(m.confFile)
	if string(active) != string(original) || current.Profiles[0].Content != s.Profiles[0].Content || current.Profiles[0].LastError == "" {
		t.Fatal("failed update replaced last known good data")
	}
}
func TestProfileMetadataAndTimezoneSurviveReload(t *testing.T) {
	m, s := managerFixture(t)
	s.Profiles[0].Cron = "30 12 * * *"
	s.Profiles[0].URL = "https://example.test/sub"
	s.Profiles[0].AttemptedAt = time.Date(2026, 10, 31, 3, 0, 0, 0, time.UTC)
	_ = m.save(s)
	restored := newProfileManager(m.confFile)
	loaded, err := restored.read()
	if err != nil {
		t.Fatal(err)
	}
	next := nextUpdate(loaded.Profiles[0], loaded.Timezone)
	if !next.Equal(time.Date(2026, 10, 31, 4, 30, 0, 0, time.UTC)) {
		t.Fatalf("wrong timezone: %s", next)
	}
}
func TestActiveScheduledUpdateAndProviderMetadata(t *testing.T) {
	m, s := managerFixture(t)
	calls := 0
	applied := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("subscription-userinfo", "upload=1; download=2; total=10; expire=1800000000")
		fmt.Fprint(w, "rules: ['MATCH,DIRECT']\nproxies: []\n")
	}))
	defer upstream.Close()
	s.Profiles[0].URL = upstream.URL
	s.Profiles[0].Cron = "* * * * *"
	s.Profiles[0].AttemptedAt = time.Now().Add(-2 * time.Minute)
	_ = m.save(s)
	m.apply = func([]byte) error { applied++; return nil }
	m.tick(time.Now())
	m.tick(time.Now())
	state, _ := m.read()
	if calls != 1 || applied != 1 || state.Profiles[0].Usage.Total != 10 {
		t.Fatal("scheduled update duplicated or failed")
	}
}
func TestRuntimeBoundaryOverridesSubscription(t *testing.T) {
	_, s := managerFixture(t)
	s.GlobalMerge = "external-controller: 0.0.0.0:9999\nsecret: wrong\ntun:\n  enable: true\n"
	data, err := effectiveConfig(s, s.Profiles[0])
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]interface{}
	_ = yaml.Unmarshal(data, &c)
	if c["external-controller"] != "127.0.0.1:19097" || c["secret"] != "inner" || c["tun"].(map[string]interface{})["enable"] != false {
		t.Fatal("subscription changed protected deployment settings")
	}
	if strings.Contains(string(data), "external-ui:") {
		t.Fatal("core exposes unprotected UI")
	}
}
func TestCustomDownloadProxyIsActuallyUsed(t *testing.T) {
	used := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { used = true; fmt.Fprint(w, "rules: ['MATCH,DIRECT']\n") }))
	defer proxy.Close()
	p := Profile{Name: "proxy", URL: "http://unreachable.invalid/sub", UseProxy: true, ProxyURL: proxy.URL, Timeout: 1, UserAgent: "test"}
	if err := fetchProfile(&p, nil); err != nil || !used {
		t.Fatal("custom download proxy ignored")
	}
}

func TestReorderRejectsMissingOrDuplicateProfiles(t *testing.T) {
	m, s := managerFixture(t)
	two := s.Profiles[0]
	two.ID = "two"
	two.Name = "Two"
	s.Profiles = append(s.Profiles, two)
	_ = m.save(s)
	for _, body := range []string{`{"ids":["one"]}`, `{"ids":["one","one"]}`, `{"ids":["missing","one"]}`} {
		req := httptest.NewRequest("PUT", "/api/profiles/order", strings.NewReader(body))
		rec := httptest.NewRecorder()
		m.handleProfiles(rec, req)
		after, _ := m.read()
		if rec.Code != 400 || after.Profiles[0].ID != "one" || len(after.Profiles) != 2 {
			t.Fatal("invalid order changed store")
		}
	}
	req := httptest.NewRequest("PUT", "/api/profiles/order", strings.NewReader(`{"ids":["two","one"]}`))
	rec := httptest.NewRecorder()
	m.handleProfiles(rec, req)
	after, _ := m.read()
	if rec.Code != 200 || after.Profiles[0].ID != "two" || after.ActiveID != "one" {
		t.Fatal("reorder changed active profile")
	}
}

func TestRemoteRawContentIsReadOnly(t *testing.T) {
	m, s := managerFixture(t)
	s.Profiles[0].URL = "https://example.test/sub"
	_ = m.save(s)
	before, _ := os.ReadFile(m.path)
	for _, section := range []string{"", "rules", "proxies", "proxy-groups"} {
		body := fmt.Sprintf(`{"section":%q,"content":"rules: ['MATCH,REJECT']"}`, section)
		rec := httptest.NewRecorder()
		m.handleProfiles(rec, httptest.NewRequest("PUT", "/api/profiles/one/content", strings.NewReader(body)))
		after, _ := os.ReadFile(m.path)
		if rec.Code != 403 || string(before) != string(after) {
			t.Fatal("remote source was modified")
		}
	}
	rec := httptest.NewRecorder()
	m.handleProfiles(rec, httptest.NewRequest("GET", "/api/profiles/one/content", nil))
	if rec.Code != 200 {
		t.Fatal("raw view unavailable")
	}
	s.Profiles[0].URL = ""
	_ = m.save(s)
	rec = httptest.NewRecorder()
	m.handleProfiles(rec, httptest.NewRequest("PUT", "/api/profiles/one/content", strings.NewReader(`{"content":"rules: ['MATCH,REJECT']"}`)))
	if rec.Code != 200 {
		t.Fatal("local editing unavailable")
	}
}

func TestRuntimeSnapshotIncludesLiveParameters(t *testing.T) {
	m, _ := managerFixture(t)
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer inner" {
			t.Error("runtime auth missing")
		}
		fmt.Fprint(w, `{"mode":"global","mixed-port":7897}`)
	}))
	defer core.Close()
	t.Setenv("EXTERNAL_PORT", strings.Split(core.URL, ":")[2])
	_ = os.WriteFile(m.confFile, []byte("mode: rule\nsecret: inner\nrules: ['MATCH,DIRECT']\n"), 0600)
	content, err := m.runtimeConfig()
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]interface{}
	_ = yaml.Unmarshal([]byte(content), &cfg)
	if cfg["mode"] != "global" || cfg["mixed-port"] != 7897 || cfg["secret"] != "[已隐藏]" || len(cfg["rules"].([]interface{})) != 1 {
		t.Fatal("runtime view is stale or leaked controller secret")
	}
}
