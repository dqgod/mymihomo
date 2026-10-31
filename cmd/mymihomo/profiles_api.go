package main

import (
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"net/http"
	"os"
	"strings"
	"time"
)

func decodeRequest(w http.ResponseWriter, r *http.Request, value interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, 12<<20)
	if err := json.NewDecoder(r.Body).Decode(value); err != nil {
		return fmt.Errorf("请求格式无效或内容过大")
	}
	return nil
}
func (m *profileManager) handleProfiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/profiles"), "/")
	if r.Method == http.MethodGet {
		if path == "runtime" {
			m.mu.Lock()
			defer m.mu.Unlock()
			content, err := m.runtimeConfig()
			if err != nil {
				respondError(w, 502, err)
				return
			}
			respondJSON(w, 200, map[string]string{"content": content})
			return
		}
		s, err := m.snapshot()
		if err != nil {
			respondError(w, 500, err)
			return
		}
		if path == "" {
			items := []map[string]interface{}{}
			for _, p := range s.Profiles {
				p.Content = ""
				b, _ := json.Marshal(p)
				var item map[string]interface{}
				_ = json.Unmarshal(b, &item)
				item["next_update"] = nextUpdate(p, s.Timezone)
				items = append(items, item)
			}
			respondJSON(w, 200, map[string]interface{}{"profiles": items, "active_id": s.ActiveID, "timezone": s.Timezone, "global_merge": s.GlobalMerge, "global_script": s.GlobalScript})
			return
		}
		parts := strings.Split(path, "/")
		i := profileIndex(s, parts[0])
		if i < 0 {
			respondError(w, 404, fmt.Errorf("订阅不存在"))
			return
		}
		p := s.Profiles[i]
		if len(parts) > 1 && parts[1] == "export" {
			w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
			w.Header().Set("Content-Disposition", `attachment; filename="profile-`+p.ID+`.yaml"`)
			_, _ = w.Write([]byte(p.Content))
			return
		}
		if len(parts) > 1 && parts[1] == "content" {
			section := r.URL.Query().Get("section")
			content := p.Content
			if section != "" {
				if !editableSection(section) {
					respondError(w, 400, fmt.Errorf("不支持的配置区域"))
					return
				}
				cfg, err := parseConfig(content)
				if err != nil {
					respondError(w, 400, err)
					return
				}
				b, _ := yaml.Marshal(cfg[section])
				content = string(b)
			}
			respondJSON(w, 200, map[string]string{"content": content})
			return
		}
		respondJSON(w, 200, p)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.read()
	if err != nil {
		respondError(w, 500, err)
		return
	}
	var requestErr error
	if path == "update-active" && r.Method == http.MethodPost {
		requestErr = m.updateLocked(s.ActiveID, nil)
	} else if path == "order" && r.Method == http.MethodPut {
		var input struct {
			IDs []string `json:"ids"`
		}
		requestErr = decodeRequest(w, r, &input)
		if requestErr == nil {
			ordered := make([]Profile, 0, len(s.Profiles))
			seen := map[string]bool{}
			for _, id := range input.IDs {
				i := profileIndex(s, id)
				if i < 0 || seen[id] {
					requestErr = fmt.Errorf("订阅排序无效")
					break
				}
				seen[id] = true
				ordered = append(ordered, s.Profiles[i])
			}
			if len(ordered) != len(s.Profiles) {
				requestErr = fmt.Errorf("订阅列表已变化，请重新读取后排序")
			}
			if requestErr == nil {
				s.Profiles = ordered
				requestErr = m.save(s)
			}
		}
	} else if path == "global" && r.Method == http.MethodPut {
		var input struct {
			Merge    string `json:"merge"`
			Script   string `json:"script"`
			Timezone string `json:"timezone"`
		}
		requestErr = decodeRequest(w, r, &input)
		if requestErr == nil {
			if input.Timezone == "" {
				input.Timezone = s.Timezone
			}
			if _, e := time.LoadLocation(input.Timezone); e != nil {
				requestErr = fmt.Errorf("时区名称无效")
			} else {
				s.GlobalMerge = input.Merge
				s.GlobalScript = input.Script
				if input.Timezone != s.Timezone {
					for i := range s.Profiles {
						s.Profiles[i].AttemptedAt = time.Now()
					}
				}
				s.Timezone = input.Timezone
				if s.ActiveID != "" {
					requestErr = m.commit(s, true)
				} else {
					_, requestErr = applyExtensions(map[string]interface{}{}, input.Merge, input.Script, "校验")
					if requestErr == nil {
						requestErr = m.save(s)
					}
				}
			}
		}
	} else if path == "" && r.Method == http.MethodPost {
		p := Profile{Name: "导入的订阅", Timeout: 45, Retries: 1, UserAgent: "Clash Verge/2.5.2"}
		requestErr = decodeRequest(w, r, &p)
		p.ID = newID()
		p.AttemptedAt = time.Now()
		p.UpdatedAt = time.Time{}
		p.LastError = ""
		p.Usage = nil
		if requestErr == nil {
			requestErr = validateProfile(p)
		}
		if requestErr == nil {
			requestErr = fetchProfile(&p, nil)
		}
		if requestErr == nil {
			data, e := effectiveConfig(s, p)
			requestErr = e
			if requestErr == nil {
				requestErr = m.validate(data)
			}
			if requestErr == nil {
				s.Profiles = append(s.Profiles, p)
				requestErr = m.save(s)
			}
		}
	} else {
		parts := strings.Split(path, "/")
		id := parts[0]
		i := profileIndex(s, id)
		if i < 0 {
			respondError(w, 404, fmt.Errorf("订阅不存在"))
			return
		}
		if len(parts) == 1 && r.Method == http.MethodDelete {
			if s.ActiveID == id {
				requestErr = fmt.Errorf("正在使用的订阅不能删除，请先切换到其他订阅")
			} else {
				s.Profiles = append(s.Profiles[:i], s.Profiles[i+1:]...)
				requestErr = m.save(s)
			}
		} else if len(parts) == 1 && r.Method == http.MethodPut {
			input := s.Profiles[i]
			requestErr = decodeRequest(w, r, &input)
			if requestErr == nil {
				requestErr = validateProfile(input)
			}
			if requestErr == nil {
				p := &s.Profiles[i]
				p.Name = input.Name
				p.URL = input.URL
				p.Cron = input.Cron
				p.UseProxy = input.UseProxy
				p.ProxyURL = input.ProxyURL
				p.Timeout = input.Timeout
				p.Retries = input.Retries
				p.Base64 = input.Base64
				p.UserAgent = input.UserAgent
				p.AttemptedAt = time.Now()
				requestErr = m.save(s)
			}
		} else if len(parts) == 2 && parts[1] == "update" && r.Method == http.MethodPost {
			var input struct {
				UseProxy *bool `json:"use_proxy"`
			}
			requestErr = decodeRequest(w, r, &input)
			if requestErr == nil {
				requestErr = m.updateLocked(id, input.UseProxy)
			}
		} else if len(parts) == 2 && parts[1] == "activate" && r.Method == http.MethodPost {
			s.ActiveID = id
			requestErr = m.commit(s, true)
		} else if len(parts) == 2 && parts[1] == "extensions" && r.Method == http.MethodPut {
			var input struct {
				Merge  string `json:"merge"`
				Script string `json:"script"`
			}
			requestErr = decodeRequest(w, r, &input)
			if requestErr == nil {
				s.Profiles[i].Merge = input.Merge
				s.Profiles[i].Script = input.Script
				data, e := effectiveConfig(s, s.Profiles[i])
				requestErr = e
				if requestErr == nil {
					requestErr = m.validate(data)
				}
				if requestErr == nil {
					requestErr = m.commit(s, s.ActiveID == id)
				}
			}
		} else if len(parts) == 2 && parts[1] == "content" && r.Method == http.MethodPut {
			if s.Profiles[i].URL != "" {
				respondError(w, 403, fmt.Errorf("远程订阅原始内容只读，请使用覆写配置或扩展脚本修改"))
				return
			}
			var input struct {
				Section string `json:"section"`
				Content string `json:"content"`
			}
			requestErr = decodeRequest(w, r, &input)
			if requestErr == nil {
				content := input.Content
				if input.Section != "" {
					if !editableSection(input.Section) {
						requestErr = fmt.Errorf("不支持的配置区域")
					} else {
						var cfg map[string]interface{}
						cfg, requestErr = parseConfig(s.Profiles[i].Content)
						var section []interface{}
						if requestErr == nil {
							requestErr = yaml.Unmarshal([]byte(input.Content), &section)
						}
						if requestErr == nil {
							cfg[input.Section] = section
							b, e := yaml.Marshal(cfg)
							requestErr = e
							content = string(b)
						}
					}
				}
				if requestErr == nil {
					s.Profiles[i].Content = content
					data, e := effectiveConfig(s, s.Profiles[i])
					requestErr = e
					if requestErr == nil {
						requestErr = m.validate(data)
					}
					if requestErr == nil {
						requestErr = m.commit(s, s.ActiveID == id)
					}
				}
			}
		} else {
			respondError(w, 405, fmt.Errorf("不支持的操作"))
			return
		}
	}
	if requestErr != nil {
		respondError(w, 400, fmt.Errorf("%s", safeMessage(requestErr)))
		return
	}
	respondJSON(w, 200, map[string]string{"message": "操作成功"})
}
func editableSection(s string) bool { return s == "rules" || s == "proxies" || s == "proxy-groups" }

// The saved config is the last successfully applied full YAML. Overlay the live
// core's mutable general settings so changes made in zashboard are reflected.
func (m *profileManager) runtimeConfig() (string, error) {
	data, err := os.ReadFile(m.confFile)
	if err != nil {
		return "", fmt.Errorf("无法读取已应用配置")
	}
	cfg, err := parseConfig(string(data))
	if err != nil {
		return "", err
	}
	req, _ := http.NewRequest("GET", "http://127.0.0.1:"+getEnvDefault("EXTERNAL_PORT", "19097")+"/configs", nil)
	req.Header.Set("Authorization", "Bearer "+os.Getenv("EXTERNAL_SECRET"))
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("无法读取内核实时参数，请稍后重试")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return "", fmt.Errorf("内核实时配置读取失败")
	}
	var live map[string]interface{}
	if err := json.NewDecoder(response.Body).Decode(&live); err != nil {
		return "", fmt.Errorf("内核实时配置无效")
	}
	for _, key := range []string{"port", "socks-port", "redir-port", "tproxy-port", "mixed-port", "allow-lan", "bind-address", "mode", "log-level", "ipv6", "tcp-concurrent", "interface-name", "routing-mark", "unified-delay", "find-process-mode"} {
		if value, ok := live[key]; ok {
			cfg[key] = value
		}
	}
	if value, ok := live["sniffing"]; ok {
		_ = mergeConfig(cfg, map[string]interface{}{"sniffer": map[string]interface{}{"enable": value}})
	}
	if value, ok := live["tun"].(map[string]interface{}); ok {
		_ = mergeConfig(cfg, map[string]interface{}{"tun": value})
	}
	// A viewer does not need the controller credential.
	if _, ok := cfg["secret"]; ok {
		cfg["secret"] = "[已隐藏]"
	}
	output, err := yaml.Marshal(cfg)
	return string(output), err
}
