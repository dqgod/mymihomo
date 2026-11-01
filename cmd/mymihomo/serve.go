package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type gateway struct {
	secret       string
	passwordHash string
	cliToken     string
	websockets   map[net.Conn]struct{}
	manager      *profileManager
	sessions     map[[32]byte]time.Time
	mu           sync.Mutex
	proxy        *httputil.ReverseProxy
	ui           http.Handler
}

func newGateway(manager *profileManager, secret, uiDir, coreURL string) *gateway {
	target, _ := url.Parse(coreURL)
	reverse := httputil.NewSingleHostReverseProxy(target)
	director := reverse.Director
	reverse.Director = func(r *http.Request) {
		director(r)
		r.Header.Set("Authorization", "Bearer "+os.Getenv("EXTERNAL_SECRET"))
		r.Header.Del("Cookie")
		r.Header.Del("X-Mymihomo-CLI-Token")
		q := r.URL.Query()
		q.Del("token")
		r.URL.RawQuery = q.Encode()
	}
	reverse.Transport = &http.Transport{Proxy: nil}
	reverse.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		respondError(w, 502, fmt.Errorf("Mihomo 暂未就绪，请稍后重试"))
	}
	return &gateway{websockets: map[net.Conn]struct{}{}, secret: secret, manager: manager, sessions: map[[32]byte]time.Time{}, proxy: reverse, ui: http.StripPrefix("/ui/zashboard/", http.FileServer(http.Dir(uiDir)))}
}

func serveConfigAPI(addr, confFile, _ string) error {
	secret := getEnvDefault("WEB_CONFIG_SECRET", os.Getenv("EXTERNAL_SECRET"))

	m := newProfileManager(confFile)
	if err := m.initialize(); err != nil {
		return err
	}
	g := newGateway(m, secret, getEnvDefault("UI_DIR", filepath.Join(getEnvDefault("MIHOMO_HOME", "/root/.config/mihomo"), "ui", "zashboard")), "http://127.0.0.1:"+getEnvDefault("EXTERNAL_PORT", "19097"))
	if err := g.loadAuthentication(); err != nil {
		return err
	}
	go m.runScheduler(context.Background())
	server := &http.Server{Addr: addr, Handler: g, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 1 << 20}
	fmt.Printf("zashboard 登录入口已启动: %s\n", addr)
	return server.ListenAndServe()
}

func (g *gateway) authorized(r *http.Request) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.authorizedLocked(r)
}
func (g *gateway) authorizedLocked(r *http.Request) bool {
	if internalCLI(r, g.cliToken) {
		return true
	}
	// Explicit bearer authentication is for local CLI/API clients. The browser uses
	// an HttpOnly cookie and never receives the Mihomo core's secret.
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer ") && g.verifyPasswordLocked(strings.TrimPrefix(header, "Bearer ")) {
		return true
	}
	cookie, err := r.Cookie("mymihomo_session")
	if err != nil {
		return false
	}
	key := sha256.Sum256([]byte(cookie.Value))
	until, ok := g.sessions[key]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(g.sessions, key)
		return false
	}
	return true
}
func validOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, r.Host)
}
func cookieFor(r *http.Request, value string, age int) *http.Cookie {
	return &http.Cookie{Name: "mymihomo_session", Value: value, Path: "/", MaxAge: age, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode}
}
func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" && r.Method != "HEAD" || strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		if !validOrigin(r) {
			respondError(w, 403, fmt.Errorf("请求来源不匹配"))
			return
		}
	}
	if r.URL.Path == "/healthz" {
		respondJSON(w, 200, map[string]string{"status": "ok"})
		return
	}
	// Retire an old dashboard service worker so it cannot bypass the login page
	// by returning its previously cached application shell.
	if r.URL.Path == "/ui/zashboard/sw.js" {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(`self.addEventListener('install',()=>self.skipWaiting());self.addEventListener('activate',event=>event.waitUntil((async()=>{for(const key of await caches.keys())if(key.startsWith('workbox'))await caches.delete(key);await self.registration.unregister();for(const client of await self.clients.matchAll({type:'window'}))client.navigate('/login');})()));`))
		return
	}
	if r.URL.Path == "/login" && r.Method == http.MethodGet {
		if g.authorized(r) {
			http.Redirect(w, r, "/ui/zashboard/", http.StatusSeeOther)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(loginPage))
		return
	}
	if r.URL.Path == "/auth/login" && r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			respondError(w, 400, fmt.Errorf("登录请求无效"))
			return
		}
		g.mu.Lock()
		if !g.verifyPasswordLocked(r.FormValue("password")) {
			g.mu.Unlock()
			http.Redirect(w, r, "/login?error=1", http.StatusSeeOther)
			return
		}
		token := newID() + newID()
		for key, until := range g.sessions {
			if time.Now().After(until) {
				delete(g.sessions, key)
			}
		}
		if len(g.sessions) >= 1024 {
			g.mu.Unlock()
			respondError(w, 429, fmt.Errorf("登录会话过多"))
			return
		}
		g.sessions[sha256.Sum256([]byte(token))] = time.Now().Add(12 * time.Hour)
		g.mu.Unlock()
		http.SetCookie(w, cookieFor(r, token, 12*60*60))
		http.Redirect(w, r, "/ui/zashboard/", http.StatusSeeOther)
		return
	}
	if !g.authorized(r) {
		if r.Method == http.MethodGet && (r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/ui")) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		} else {
			respondError(w, 401, fmt.Errorf("请先登录 zashboard"))
		}
		return
	}
	if r.URL.Path == "/auth/password" && r.Method == http.MethodPost {
		g.changePassword(w, r)
		return
	}
	if r.URL.Path == "/auth/logout" && r.Method == http.MethodPost {
		if cookie, err := r.Cookie("mymihomo_session"); err == nil {
			g.mu.Lock()
			delete(g.sessions, sha256.Sum256([]byte(cookie.Value)))
			g.mu.Unlock()
		}
		http.SetCookie(w, cookieFor(r, "", -1))
		respondJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.URL.Path == "/auth/session" {
		respondJSON(w, 200, map[string]interface{}{"authenticated": true, "managed": true})
		return
	}
	if strings.TrimRight(r.URL.Path, "/") == "/rules/disable" && r.Method != http.MethodGet {
		respondError(w, 409, fmt.Errorf("请在规则编辑页启用或禁用，变更会持久保存"))
		return
	}
	if r.URL.Path == "/api/runtime-edits" {
		g.manager.handleRuntimeEdits(w, r)
		return
	}
	if r.URL.Path == "/api/profiles" || strings.HasPrefix(r.URL.Path, "/api/profiles/") {
		g.manager.handleProfiles(w, r)
		return
	}
	if r.URL.Path == "/api/config" {
		respondError(w, 410, fmt.Errorf("配置管理已迁移到 zashboard 的订阅页"))
		return
	}
	if r.URL.Path == "/" || r.URL.Path == "/ui" || r.URL.Path == "/ui/" || r.URL.Path == "/ui/zashboard" {
		http.Redirect(w, r, "/ui/zashboard/", http.StatusSeeOther)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/ui/zashboard/") {
		g.ui.ServeHTTP(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/ui/") {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/upgrade") || r.URL.Path == "/restart" || (strings.TrimRight(r.URL.Path, "/") == "/configs" && r.Method == http.MethodPut) {
		respondError(w, 409, fmt.Errorf("此实例由 mymihomo 管理：配置请在订阅页更新，程序升级请重新构建镜像"))
		return
	}
	if strings.TrimRight(r.URL.Path, "/") == "/configs" && r.Method == http.MethodPatch {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		var changes map[string]interface{}
		if err != nil || json.Unmarshal(data, &changes) != nil {
			respondError(w, 400, fmt.Errorf("配置请求无效"))
			return
		}
		for _, key := range []string{"port", "socks-port", "mixed-port", "redir-port", "tproxy-port", "external-controller", "secret"} {
			if _, exists := changes[key]; exists {
				respondError(w, 409, fmt.Errorf("端口和密钥由 Docker 部署管理，请修改 .env 与 compose.local.yaml 后重建容器"))
				return
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(data))
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		g.proxy.ServeHTTP(&authenticatedUpgrade{ResponseWriter: w, g: g, request: r}, r)
	} else {
		g.proxy.ServeHTTP(w, r)
	}
}

func respondError(w http.ResponseWriter, code int, err error) {
	respondJSON(w, code, map[string]string{"error": safeMessage(err)})
}
func respondJSON(w http.ResponseWriter, code int, value interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

const loginPage = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>登录 · zashboard</title><style>
:root{color-scheme:light dark;font-family:system-ui,sans-serif;color:#172554;background:#f1f5f9}*{box-sizing:border-box}body{margin:0;min-height:100dvh;display:grid;place-items:center;padding:24px;background:radial-gradient(ellipse at top,#dbeafe,transparent 65%)}main{background:#fff;border:1px solid #e2e8f0;border-radius:24px;padding:36px;width:100%;max-width:420px;box-shadow:0 24px 60px #0f172a0d}.logo{display:grid;place-items:center;width:48px;height:48px;border-radius:14px;background:#2563eb;color:#fff;font-size:26px;font-weight:700}h1{font-size:26px;margin:22px 0 8px}p{color:#64748b;line-height:1.7;margin-bottom:26px;font-size:14px}label{display:block;margin-bottom:8px;font-size:14px;font-weight:600}input,button{font:inherit;width:100%;border-radius:10px;padding:12px 14px}input{border:1px solid #cbd5e1;background:transparent;color:inherit;outline:none}input:focus{border-color:#2563eb;box-shadow:0 0 0 3px #2563eb22}button{margin-top:18px;border:0;background:#2563eb;color:white;font-weight:600;cursor:pointer}.error{display:none;color:#dc2626;font-size:13px;margin-top:10px}.foot{font-size:12px;color:#94a3b8;margin-top:22px;text-align:center}@media(prefers-color-scheme:dark){:root{color:#e2e8f0;background:#0f172a}body{background:radial-gradient(ellipse at top,#172554,transparent 65%)}main{background:#111c30;border-color:#243247}input{border-color:#475569}p{color:#94a3b8}}
</style></head><body><main><div class="logo">z</div><h1>登录 zashboard</h1><p>管理代理、订阅与扩展配置。</p><p id="changed" style="display:none;color:#16a34a" role="status">密码已修改，请使用新密码登录。</p><form action="/auth/login" method="post"><label for="password">访问密码</label><input id="password" name="password" type="password" autocomplete="current-password" required autofocus><div class="error" id="error" role="alert">密码不正确，请重试。</div><button type="submit">登录</button></form><div class="foot">mymihomo · 私人代理控制台</div></main><script>if(new URLSearchParams(location.search).has('changed'))document.getElementById('changed').style.display='block';if(new URLSearchParams(location.search).has('error'))document.getElementById('error').style.display='block';if('serviceWorker'in navigator)navigator.serviceWorker.getRegistrations().then(rs=>rs.forEach(r=>r.unregister()));</script></body></html>`
