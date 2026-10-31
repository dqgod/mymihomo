package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"
)

func (m *profileManager) validateRuntime(data []byte) error {
	path, err := stageFile(m.confFile, data)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, getEnvDefault("MIHOMO_BINARY", "/mihomo"), "-d", getEnvDefault("MIHOMO_HOME", "/root/.config/mihomo"), "-f", path, "-t")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("内核校验失败，配置未应用：%s", safeMessage(fmt.Errorf("%s", output)))
	}
	return nil
}

func applyRuntime(data []byte) error {
	body, _ := json.Marshal(map[string]string{"payload": string(data)})
	req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1:"+getEnvDefault("EXTERNAL_PORT", "19097")+"/configs?force=true", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("EXTERNAL_SECRET"))
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 30 * time.Second}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("内核连接失败，原配置已保留")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return fmt.Errorf("内核拒绝应用配置 (HTTP %d)，原配置已保留", resp.StatusCode)
	}
	return nil
}

func prepareManagedConfig(confFile string) error {
	m := newProfileManager(confFile)
	if err := m.initialize(); err != nil {
		return err
	}
	s, err := m.read()
	if err != nil {
		return err
	}
	if len(s.Profiles) == 0 && os.Getenv("CONF_URL") != "" {
		p := Profile{ID: newID(), Name: "当前订阅", URL: os.Getenv("CONF_URL"), Cron: getEnvDefault("CRON_EXPRESSION", "1 * * * *"), Timeout: 45, Retries: 1, UseProxy: os.Getenv("HTTPS_PROXY") != "", UserAgent: getEnvDefault("CLASH_SUB_UA", "Clash Verge/2.5.2"), AttemptedAt: time.Now()}
		if err := fetchProfile(&p, nil); err == nil {
			s.Profiles = append(s.Profiles, p)
			s.ActiveID = p.ID
		} else {
			fmt.Println("首次订阅下载未成功，请登录 zashboard 导入订阅")
		}
	}
	var data []byte
	if index := profileIndex(s, s.ActiveID); index >= 0 {
		data, err = effectiveConfig(s, s.Profiles[index])
	} else {
		data, err = effectiveConfig(s, Profile{Name: "引导配置", Content: "proxies: []\nproxy-groups: []\nrules: ['MATCH,DIRECT']\n"})
	}
	if err != nil {
		return err
	}
	if err = m.validate(data); err != nil {
		return err
	}
	if err = atomicWrite(confFile, data); err != nil {
		return err
	}
	return m.save(s)
}

// All update paths share the server lock and the persistent profile store.
func updateConfig(confFile string) error {
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+getEnvDefault("WEB_CONFIG_PORT", "9097")+"/api/profiles/update-active", bytes.NewBufferString("{}"))
	token, err := os.ReadFile(cliTokenPath(confFile))
	if err != nil {
		return fmt.Errorf("本机命令行认证不可用，请检查管理服务")
	}
	req.Header.Set("X-Mymihomo-CLI-Token", string(token))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 10 * time.Minute}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("订阅管理服务不可用")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("更新失败 (HTTP %d): %s", resp.StatusCode, b)
	}
	fmt.Println("订阅配置已更新并应用")
	return nil
}
