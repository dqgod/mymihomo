package main

import (
	"bufio"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

type authRecord struct {
	Version      int    `json:"version"`
	PasswordHash string `json:"password_hash"`
}

func authPath(confFile string) string { return filepath.Join(filepath.Dir(confFile), "auth.json") }
func cliTokenPath(confFile string) string {
	return filepath.Join(filepath.Dir(confFile), ".auth-cli-token")
}
func (g *gateway) loadAuthentication() error {
	data, err := os.ReadFile(authPath(g.manager.confFile))
	if err == nil {
		var record authRecord
		if json.Unmarshal(data, &record) != nil || record.Version != 1 {
			return fmt.Errorf("访问密码文件无效")
		}
		if _, err := bcrypt.Cost([]byte(record.PasswordHash)); err != nil {
			return fmt.Errorf("访问密码摘要无效")
		}
		g.passwordHash = record.PasswordHash
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("无法读取访问密码文件")
	}
	if g.passwordHash == "" && g.secret == "" {
		return fmt.Errorf("请设置 WEB_CONFIG_SECRET 初始访问密码")
	}
	token, err := os.ReadFile(cliTokenPath(g.manager.confFile))
	if os.IsNotExist(err) {
		token = []byte(newID() + newID())
		err = atomicWrite(cliTokenPath(g.manager.confFile), token)
	}
	if err != nil || len(token) != 64 {
		return fmt.Errorf("无法初始化本机命令行认证")
	}
	g.cliToken = string(token)
	return nil
}
func (g *gateway) verifyPasswordLocked(password string) bool {
	if g.passwordHash != "" {
		return bcrypt.CompareHashAndPassword([]byte(g.passwordHash), []byte(password)) == nil
	}
	return g.secret != "" && subtle.ConstantTimeCompare([]byte(password), []byte(g.secret)) == 1
}
func (g *gateway) changePassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
		Confirm string `json:"confirm_password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		respondError(w, 400, fmt.Errorf("密码请求无效"))
		return
	}
	if input.New != input.Confirm {
		respondError(w, 400, fmt.Errorf("两次新密码不一致"))
		return
	}
	if utf8.RuneCountInString(input.New) < 8 || len(input.New) > 72 {
		respondError(w, 400, fmt.Errorf("新密码至少 8 个字符，UTF-8 编码不超过 72 字节"))
		return
	}
	g.mu.Lock()
	if !g.verifyPasswordLocked(input.Current) {
		g.mu.Unlock()
		respondError(w, 400, fmt.Errorf("当前密码不正确"))
		return
	}
	if input.Current == input.New {
		g.mu.Unlock()
		respondError(w, 400, fmt.Errorf("新密码不能与当前密码相同"))
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.New), bcrypt.DefaultCost)
	if err != nil {
		g.mu.Unlock()
		respondError(w, 500, fmt.Errorf("密码处理失败"))
		return
	}
	data, _ := json.Marshal(authRecord{Version: 1, PasswordHash: string(hash)})
	if err := atomicWrite(authPath(g.manager.confFile), data); err != nil {
		g.mu.Unlock()
		respondError(w, 500, fmt.Errorf("密码保存失败，原密码仍然有效"))
		return
	}
	g.passwordHash = string(hash)
	g.sessions = map[[32]byte]time.Time{}
	sockets := make([]net.Conn, 0, len(g.websockets))
	for conn := range g.websockets {
		sockets = append(sockets, conn)
	}
	g.mu.Unlock()
	for _, conn := range sockets {
		_ = conn.Close()
	}
	http.SetCookie(w, cookieFor(r, "", -1))
	respondJSON(w, 200, map[string]string{"message": "密码已修改，请重新登录"})
}

// Upgrade connections are tracked so password changes also revoke existing
// WebSocket streams, not only subsequent HTTP requests.
type authenticatedUpgrade struct {
	http.ResponseWriter
	g       *gateway
	request *http.Request
}
type authenticatedConn struct {
	net.Conn
	g *gateway
}

func (c *authenticatedConn) Close() error {
	c.g.mu.Lock()
	delete(c.g.websockets, c)
	c.g.mu.Unlock()
	return c.Conn.Close()
}
func (w *authenticatedUpgrade) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.g.mu.Lock()
	defer w.g.mu.Unlock()
	if !w.g.authorizedLocked(w.request) {
		return nil, nil, fmt.Errorf("会话已过期")
	}
	conn, rw, err := w.ResponseWriter.(http.Hijacker).Hijack()
	if err != nil {
		return nil, nil, err
	}
	wrapped := &authenticatedConn{Conn: conn, g: w.g}
	w.g.websockets[wrapped] = struct{}{}
	return wrapped, rw, nil
}
func (w *authenticatedUpgrade) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func internalCLI(r *http.Request, token string) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	return err == nil && ip != nil && ip.IsLoopback() && token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Mymihomo-CLI-Token")), []byte(token)) == 1
}
