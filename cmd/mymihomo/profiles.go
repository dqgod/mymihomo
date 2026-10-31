package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

const maxConfigBytes = 8 << 20

type SubscriptionUsage struct {
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
	Total    int64 `json:"total"`
	Expire   int64 `json:"expire"`
}
type Profile struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	URL         string             `json:"url"`
	Homepage    string             `json:"homepage"`
	Content     string             `json:"content,omitempty"`
	Merge       string             `json:"merge"`
	Script      string             `json:"script"`
	Cron        string             `json:"cron"`
	UseProxy    bool               `json:"use_proxy"`
	ProxyURL    string             `json:"proxy_url"`
	UserAgent   string             `json:"user_agent"`
	Base64      bool               `json:"base64"`
	Timeout     int                `json:"timeout"`
	Retries     int                `json:"retries"`
	UpdatedAt   time.Time          `json:"updated_at"`
	AttemptedAt time.Time          `json:"attempted_at"`
	LastError   string             `json:"last_error"`
	Usage       *SubscriptionUsage `json:"usage,omitempty"`
}
type ProfileState struct {
	Version      int       `json:"version"`
	ActiveID     string    `json:"active_id"`
	Timezone     string    `json:"timezone"`
	GlobalMerge  string    `json:"global_merge"`
	GlobalScript string    `json:"global_script"`
	Profiles     []Profile `json:"profiles"`
}
type profileManager struct {
	mu       sync.Mutex
	path     string
	confFile string
	validate func([]byte) error
	apply    func([]byte) error
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func profilePath(confFile string) string {
	return filepath.Join(filepath.Dir(confFile), "profiles.json")
}
func newProfileManager(confFile string) *profileManager {
	m := &profileManager{path: profilePath(confFile), confFile: confFile}
	m.validate = m.validateRuntime
	m.apply = applyRuntime
	return m
}
func stageFile(path string, data []byte) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mymihomo-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(name)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}
func atomicWrite(path string, data []byte) error {
	name, err := stageFile(path, data)
	if err != nil {
		return err
	}
	defer os.Remove(name)
	return os.Rename(name, path)
}
func (m *profileManager) read() (ProfileState, error) {
	var s ProfileState
	b, err := os.ReadFile(m.path)
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("订阅存储无法读取")
	}
	if s.Version != 1 {
		return s, fmt.Errorf("不支持的订阅存储版本")
	}
	return s, nil
}
func (m *profileManager) save(s ProfileState) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(m.path, b)
}
func profileIndex(s ProfileState, id string) int {
	for i, p := range s.Profiles {
		if p.ID == id {
			return i
		}
	}
	return -1
}
func (m *profileManager) initialize() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := os.Stat(m.path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	legacy, _ := readEnvFile(getWebEnvFile())
	get := func(k, def string) string {
		if v, ok := legacy[k]; ok {
			return v
		}
		return getEnvDefault(k, def)
	}
	legacyInt := func(key string, fallback int) int {
		v, err := strconv.Atoi(get(key, strconv.Itoa(fallback)))
		if err != nil {
			return fallback
		}
		return v
	}
	s := ProfileState{Version: 1, Timezone: getEnvDefault("TZ", "Asia/Hong_Kong"), Profiles: []Profile{}}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		s.Timezone = "Asia/Hong_Kong"
	}
	if b, err := os.ReadFile(m.confFile); err == nil && len(b) > 0 {
		if _, err = parseConfig(string(b)); err != nil {
			return err
		}
		p := Profile{ID: newID(), Name: "当前订阅", URL: get("CONF_URL", ""), Content: string(b), Cron: get("CRON_EXPRESSION", "1 * * * *"), Timeout: legacyInt("CONF_TIMEOUT_SEC", 45), Retries: legacyInt("CONF_RETRY", 1), UserAgent: get("CLASH_SUB_UA", "Clash Verge/2.5.2"), UseProxy: os.Getenv("HTTPS_PROXY") != "", Base64: get("BASE64_CONVERT", "false") == "true", UpdatedAt: time.Now(), AttemptedAt: time.Now()}
		if p.URL == "" {
			p.Name = "本地配置"
			p.Cron = ""
		}
		if _, err := cron.ParseStandard(p.Cron); p.Cron != "" && err != nil {
			p.Cron = ""
		}
		s.ActiveID = p.ID
		s.Profiles = append(s.Profiles, p)
		if path := get("CUSTOM_CONF", ""); path != "" {
			if b, err := os.ReadFile(path); err == nil {
				s.GlobalMerge = string(b)
			}
		}
	}
	return m.save(s)
}
func safeMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	msg = regexp.MustCompile(`https?://[^\s"'<>]+`).ReplaceAllString(msg, "[订阅地址]")
	for _, k := range []string{"EXTERNAL_SECRET", "WEB_CONFIG_SECRET", "CONF_URL"} {
		if v := os.Getenv(k); v != "" {
			msg = strings.ReplaceAll(msg, v, "[已隐藏]")
		}
	}
	if len(msg) > 1200 {
		msg = msg[:1200]
	}
	return msg
}
func validateProfile(p Profile) error {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 160 {
		return fmt.Errorf("订阅名称不能为空，且不能超过 160 字节")
	}
	if p.URL != "" {
		u, e := url.Parse(p.URL)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("订阅链接必须使用 http:// 或 https://")
		}
	}
	if p.Cron != "" {
		if _, e := cron.ParseStandard(p.Cron); e != nil {
			return fmt.Errorf("定时表达式无效，请使用五列 Cron 表达式")
		}
	}
	if p.Timeout < 1 || p.Timeout > 120 {
		return fmt.Errorf("下载超时需要在 1–120 秒之间")
	}
	if p.Retries < 0 || p.Retries > 3 {
		return fmt.Errorf("重试次数需要在 0–3 之间")
	}
	if p.ProxyURL != "" {
		u, e := url.Parse(p.ProxyURL)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h") {
			return fmt.Errorf("下载代理地址无效")
		}
	}
	return nil
}

func fetchProfile(p *Profile, proxyOverride *bool) error {
	if p.URL == "" {
		if p.Content == "" {
			return fmt.Errorf("请填写订阅链接或导入 YAML 文件")
		}
		_, err := parseConfig(p.Content)
		if err == nil {
			p.UpdatedAt = time.Now()
			p.LastError = ""
		}
		return err
	}
	useProxy := p.UseProxy
	if proxyOverride != nil {
		useProxy = *proxyOverride
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	if useProxy {
		address := p.ProxyURL
		if address == "" {
			address = getEnvDefault("HTTPS_PROXY", getEnvDefault("HTTP_PROXY", ""))
		}
		if address == "" {
			return fmt.Errorf("未设置下载代理，请在订阅信息中填写代理地址")
		}
		u, err := url.Parse(address)
		if err != nil || u.Host == "" {
			return fmt.Errorf("下载代理地址无效")
		}
		transport.Proxy = http.ProxyURL(u)
	}
	client := &http.Client{Transport: transport, Timeout: time.Duration(p.Timeout) * time.Second}
	var last error
	for attempt := 0; attempt <= p.Retries; attempt++ {
		req, err := http.NewRequest(http.MethodGet, p.URL, nil)
		if err != nil {
			return fmt.Errorf("订阅链接无效")
		}
		req.Header.Set("User-Agent", p.UserAgent)
		res, err := client.Do(req)
		if err != nil {
			last = fmt.Errorf("下载失败：连接或请求超时，请检查订阅地址和下载代理")
		} else {
			body, readErr := io.ReadAll(io.LimitReader(res.Body, maxConfigBytes+1))
			res.Body.Close()
			if res.StatusCode != http.StatusOK {
				last = fmt.Errorf("订阅服务器返回 HTTP %d", res.StatusCode)
			} else if readErr != nil {
				last = fmt.Errorf("订阅响应读取失败")
			} else if len(body) > maxConfigBytes {
				last = fmt.Errorf("订阅文件超过 8 MiB")
			} else {
				if p.Base64 {
					body, err = decodeBase64(body)
					if err != nil {
						return fmt.Errorf("订阅 Base64 解码失败")
					}
				}
				if _, err = parseConfig(string(body)); err != nil {
					return err
				}
				p.Content = string(body)
				p.UpdatedAt = time.Now()
				p.LastError = ""
				p.Usage = parseUsage(res.Header.Get("Subscription-Userinfo"))
				if u, err := url.Parse(res.Header.Get("Profile-Web-Page-Url")); err == nil && (u.Scheme == "https" || u.Scheme == "http") {
					p.Homepage = u.String()
				}
				if p.Name == "导入的订阅" {
					if _, params, err := mime.ParseMediaType(res.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
						p.Name = strings.TrimSuffix(params["filename"], ".yaml")
					}
				}
				return nil
			}
		}
		if attempt < p.Retries {
			time.Sleep(time.Second)
		}
	}
	return last
}
func parseUsage(value string) *SubscriptionUsage {
	if value == "" {
		return nil
	}
	m := map[string]int64{}
	for _, pair := range strings.Split(value, ";") {
		parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(parts) == 2 {
			m[parts[0]], _ = strconv.ParseInt(parts[1], 10, 64)
		}
	}
	return &SubscriptionUsage{Upload: m["upload"], Download: m["download"], Total: m["total"], Expire: m["expire"]}
}
func effectiveConfig(s ProfileState, p Profile) ([]byte, error) {
	c, err := parseConfig(p.Content)
	if err != nil {
		return nil, err
	}
	c, err = applyExtensions(c, s.GlobalMerge, s.GlobalScript, p.Name)
	if err != nil {
		return nil, fmt.Errorf("全局扩展: %w", err)
	}
	c, err = applyExtensions(c, p.Merge, p.Script, p.Name)
	if err != nil {
		return nil, fmt.Errorf("订阅扩展: %w", err)
	}
	modifyConfig(c)
	// The public UI is served by the authenticated gateway, never by Mihomo.
	c["external-controller"] = "127.0.0.1:" + getEnvDefault("EXTERNAL_PORT", "19097")
	c["secret"] = os.Getenv("EXTERNAL_SECRET")
	delete(c, "external-controller-tls")
	delete(c, "external-controller-unix")
	delete(c, "external-controller-pipe")
	delete(c, "external-ui")
	delete(c, "external-ui-url")
	// TUN remains a deployment-level capability, not a subscription-side switch.
	if !getEnvBool("TUN_ENABLE", false) {
		c["tun"] = map[string]interface{}{"enable": false}
	}
	return yaml.Marshal(c)
}
func (m *profileManager) snapshot() (ProfileState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.read()
}
func (m *profileManager) commit(s ProfileState, apply bool) error {
	if !apply {
		return m.save(s)
	}
	index := profileIndex(s, s.ActiveID)
	if index < 0 {
		return fmt.Errorf("没有正在使用的订阅")
	}
	data, err := effectiveConfig(s, s.Profiles[index])
	if err != nil {
		return err
	}
	if err = m.validate(data); err != nil {
		return err
	}
	storeBytes, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	storeTemp, err := stageFile(m.path, storeBytes)
	if err != nil {
		return err
	}
	defer os.Remove(storeTemp)
	configTemp, err := stageFile(m.confFile, data)
	if err != nil {
		return err
	}
	defer os.Remove(configTemp)
	previous, err := os.ReadFile(m.confFile)
	if err != nil {
		return err
	}
	if err = m.apply(data); err != nil {
		_ = m.apply(previous)
		return err
	}
	if err = os.Rename(configTemp, m.confFile); err != nil {
		_ = m.apply(previous)
		return err
	}
	if err = os.Rename(storeTemp, m.path); err != nil {
		_ = atomicWrite(m.confFile, previous)
		_ = m.apply(previous)
		return err
	}
	return nil
}
func (m *profileManager) recordFailure(original ProfileState, id string, err error) {
	if i := profileIndex(original, id); i >= 0 {
		original.Profiles[i].AttemptedAt = time.Now()
		original.Profiles[i].LastError = safeMessage(err)
		_ = m.save(original)
	}
}
func (m *profileManager) updateLocked(id string, proxy *bool) error {
	s, err := m.read()
	if err != nil {
		return err
	}
	i := profileIndex(s, id)
	if i < 0 {
		return fmt.Errorf("订阅不存在")
	}
	originalBytes, _ := json.Marshal(s)
	var original ProfileState
	_ = json.Unmarshal(originalBytes, &original)
	p := &s.Profiles[i]
	p.AttemptedAt = time.Now()
	if err = fetchProfile(p, proxy); err == nil {
		if s.ActiveID != id {
			var data []byte
			data, err = effectiveConfig(s, *p)
			if err == nil {
				err = m.validate(data)
			}
		}
		if err == nil {
			err = m.commit(s, s.ActiveID == id)
		}
	}
	if err != nil {
		m.recordFailure(original, id, err)
	}
	return err
}
func nextUpdate(p Profile, timezone string) time.Time {
	if p.Cron == "" || p.URL == "" {
		return time.Time{}
	}
	schedule, err := cron.ParseStandard(p.Cron)
	if err != nil {
		return time.Time{}
	}
	zone, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}
	}
	base := p.AttemptedAt
	if base.IsZero() {
		base = p.UpdatedAt
	}
	if base.IsZero() {
		base = time.Now()
	}
	return schedule.Next(base.In(zone))
}
func (m *profileManager) tick(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.read()
	if err != nil {
		return
	}
	for _, p := range s.Profiles {
		next := nextUpdate(p, s.Timezone)
		if !next.IsZero() && !next.After(now) {
			_ = m.updateLocked(p.ID, nil)
		}
	}
}
func (m *profileManager) runScheduler(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			m.tick(now)
		case <-ctx.Done():
			return
		}
	}
}
