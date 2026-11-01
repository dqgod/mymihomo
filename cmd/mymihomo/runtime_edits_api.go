package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type runtimeRequest struct {
	ProfileID string   `json:"profile_id"`
	Revision  string   `json:"revision"`
	Action    string   `json:"action"`
	Kind      string   `json:"kind"`
	ID        string   `json:"id"`
	IDs       []string `json:"ids"`
	YAML      string   `json:"yaml"`
	Disabled  *bool    `json:"disabled"`
	Before    string   `json:"before"`
}

func runtimeRevision(s ProfileState, p Profile) string {
	b, _ := json.Marshal([]interface{}{p.ID, p.Name, p.Content, p.Merge, p.Script, s.GlobalMerge, s.GlobalScript, p.RuntimeEdits})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (m *profileManager) handleRuntimeEdits(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.read()
	if err != nil {
		respondError(w, 500, err)
		return
	}
	i := profileIndex(s, s.ActiveID)
	if i < 0 {
		respondError(w, 409, fmt.Errorf("请先在订阅页启用一份配置"))
		return
	}
	p := s.Profiles[i]
	base, err := baselineConfig(s, p)
	if err != nil {
		respondError(w, 400, err)
		return
	}
	rows, err := runtimeRows(base, p.RuntimeEdits)
	if err != nil {
		respondError(w, 400, err)
		return
	}
	if r.Method == http.MethodGet {
		byKind := map[string][]RuntimeRow{"groups": {}, "rules": {}, "providers": {}}
		for _, row := range rows {
			byKind[row.Kind] = append(byKind[row.Kind], row)
		}
		nodes := []string{"DIRECT", "REJECT", "REJECT-DROP", "PASS"}
		proxies, _ := base["proxies"].([]interface{})
		for _, v := range proxies {
			nodes = append(nodes, objectName(v))
		}
		for _, row := range byKind["groups"] {
			nodes = append(nodes, objectName(row.Value))
		}
		sources := []string{}
		if providers, ok := base["proxy-providers"].(map[string]interface{}); ok {
			for name := range providers {
				sources = append(sources, name)
			}
		}
		sort.Strings(sources)
		respondJSON(w, 200, map[string]interface{}{"profile_id": p.ID, "profile_name": p.Name, "revision": runtimeRevision(s, p), "groups": byKind["groups"], "rules": byKind["rules"], "providers": byKind["providers"], "proxy_options": nodes, "proxy_provider_options": sources})
		return
	}
	if r.Method != http.MethodPost {
		respondError(w, 405, fmt.Errorf("不支持的操作"))
		return
	}
	var input runtimeRequest
	if err := decodeRequest(w, r, &input); err != nil {
		respondError(w, 400, err)
		return
	}
	if input.ProfileID != p.ID || input.Revision != runtimeRevision(s, p) {
		respondError(w, 409, fmt.Errorf("配置已更新或订阅已切换，请重新读取后再操作"))
		return
	}
	if err := mutateRuntime(&p, base, rows, input); err != nil {
		respondError(w, 400, err)
		return
	}
	s.Profiles[i] = p
	if err := m.commit(s, true, true); err != nil {
		respondError(w, 400, err)
		return
	}
	respondJSON(w, 200, map[string]string{"message": "运行时配置已保存并应用"})
}
func parseRuntimeValue(kind, text string) (interface{}, error) {
	if len(text) > maxConfigBytes {
		return nil, fmt.Errorf("编辑内容过大")
	}
	if kind == "rules" {
		text = strings.TrimSpace(text)
		if text == "" || strings.ContainsAny(text, "\r\n") {
			return nil, fmt.Errorf("每条规则填写一行，规则内容不能为空")
		}
		parts := ruleParts(text)
		if len(parts) < 2 {
			return nil, fmt.Errorf("规则格式无效")
		}
		return text, nil
	}
	if kind != "groups" && kind != "providers" {
		return nil, fmt.Errorf("编辑类型无效")
	}
	var v map[string]interface{}
	if err := yaml.Unmarshal([]byte(text), &v); err != nil || v == nil {
		return nil, fmt.Errorf("请输入有效的 YAML 对象")
	}
	name := objectName(v)
	if strings.TrimSpace(name) == "" || len(name) > 200 || strings.ContainsAny(name, ",\r\n()") {
		return nil, fmt.Errorf("名称不能为空，且不能包含逗号、括号或换行")
	}
	kindType, _ := v["type"].(string)
	if kind == "groups" {
		allowed := map[string]bool{"select": true, "url-test": true, "fallback": true, "load-balance": true, "relay": true, "smart": true}
		if !allowed[kindType] {
			return nil, fmt.Errorf("请选择有效的代理组类型")
		}
		if kindType == "url-test" {
			url, _ := v["url"].(string)
			if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
				return nil, fmt.Errorf("自动选择需要填写 HTTP(S) 测速地址")
			}
		}
		for _, field := range []string{"proxies", "use"} {
			if value, exists := v[field]; exists {
				list, ok := value.([]interface{})
				if !ok {
					return nil, fmt.Errorf("%s 必须是名称列表", field)
				}
				for _, member := range list {
					if _, ok := member.(string); !ok {
						return nil, fmt.Errorf("%s 必须是名称列表", field)
					}
				}
			}
		}
	} else {
		if kindType != "http" && kindType != "file" && kindType != "inline" {
			return nil, fmt.Errorf("规则集类型必须为 http、file 或 inline")
		}
		behavior, _ := v["behavior"].(string)
		if behavior != "domain" && behavior != "ipcidr" && behavior != "classical" {
			return nil, fmt.Errorf("请选择规则集 behavior")
		}
	}
	if _, err := json.Marshal(v); err != nil {
		return nil, fmt.Errorf("配置包含无法保存的值，请检查 NaN、Infinity 或非字符串对象键")
	}
	return cloneValue(v), nil
}
func removeRuntimeMove(edits *RuntimeEdits, id string) {
	kept := []RuntimeMove{}
	for _, move := range edits.Moves {
		if move.ID != id {
			kept = append(kept, move)
		}
	}
	edits.Moves = kept
}
func rewriteStoredReferences(edits *RuntimeEdits, kind, old, name string) {
	if old == "" || name == "" || old == name {
		return
	}
	aliases := nameAliases{Groups: map[string]string{}, Providers: map[string]string{}}
	if kind == "groups" {
		aliases.Groups[old] = name
	}
	if kind == "providers" {
		aliases.Providers[old] = name
	}
	for id, e := range edits.Entries {
		e.Value = rewriteReferences(e.Kind, e.Value, aliases)
		if e.LastValue != nil {
			e.LastValue = rewriteReferences(e.Kind, e.LastValue, aliases)
		}
		edits.Entries[id] = e
	}
}
func mutateRuntime(p *Profile, base map[string]interface{}, rows []RuntimeRow, input runtimeRequest) error {
	e := &p.RuntimeEdits
	if e.Entries == nil {
		e.Entries = map[string]RuntimeEdit{}
	}
	lookup := func(id string) (RuntimeRow, error) {
		i := rowIndex(rows, id)
		if i < 0 {
			return RuntimeRow{}, fmt.Errorf("条目不存在，请重新读取")
		}
		return rows[i], nil
	}
	switch input.Action {
	case "create":
		value, err := parseRuntimeValue(input.Kind, input.YAML)
		if err != nil {
			return err
		}
		id := "added-" + newID()
		e.Entries[id] = RuntimeEdit{Kind: input.Kind, Added: true, Value: value, Disabled: input.Kind != "groups" && input.Disabled != nil && *input.Disabled}
		e.AddedOrder = append(e.AddedOrder, id)
		if input.Kind == "rules" && input.Before != "" {
			r, err := lookup(input.Before)
			if err != nil || r.Kind != "rules" {
				return fmt.Errorf("插入位置不存在")
			}
			e.Moves = append(e.Moves, RuntimeMove{ID: id, Before: input.Before})
		}
	case "edit", "toggle", "move":
		row, err := lookup(input.ID)
		if err != nil {
			return err
		}
		entry, exists := e.Entries[row.ID]
		if !exists {
			entry = RuntimeEdit{Kind: row.Kind, Value: cloneValue(row.Original), Base: cloneValue(row.Original)}
		}
		if input.Action == "edit" {
			value, err := parseRuntimeValue(row.Kind, input.YAML)
			if err != nil {
				return err
			}
			rewriteStoredReferences(e, row.Kind, objectName(row.Value), objectName(value))
			entry.Value = value
			if !entry.Added && !row.Missing {
				entry.Base = cloneValue(row.Original)
			}
			if row.Missing {
				entry.LastValue = cloneValue(value)
				if entry.LastBase != nil {
					entry.Base = cloneValue(entry.LastBase)
				}
			}
			if input.Disabled != nil && row.Kind != "groups" {
				entry.Disabled = *input.Disabled
			}
		} else if input.Action == "toggle" {
			if row.Kind == "groups" || input.Disabled == nil {
				return fmt.Errorf("仅规则和规则集支持启用/禁用")
			}
			entry.Disabled = *input.Disabled
		} else {
			if row.Kind != "rules" || row.ID == input.Before {
				return fmt.Errorf("规则排序目标无效")
			}
			if input.Before != "" {
				target, err := lookup(input.Before)
				if err != nil || target.Kind != "rules" {
					return fmt.Errorf("规则排序目标不存在")
				}
			}
			removeRuntimeMove(e, row.ID)
			e.Moves = append(e.Moves, RuntimeMove{ID: row.ID, Before: input.Before})
		}
		e.Entries[row.ID] = entry
	case "restore", "delete":
		ids := input.IDs
		if len(ids) == 0 && input.ID != "" {
			ids = []string{input.ID}
		}
		if len(ids) == 0 {
			return fmt.Errorf("请先选择条目")
		}
		// Validate every item before changing any, including mixed batch requests.
		for _, id := range ids {
			row, err := lookup(id)
			if err != nil {
				return err
			}
			if input.Action == "delete" && row.Status != "added" {
				return fmt.Errorf("批量删除只适用于新增条目，原有规则请禁用或恢复默认")
			}
			if input.Action == "restore" && row.Status == "added" {
				return fmt.Errorf("新增条目没有默认值，请使用删除")
			}
		}
		for _, id := range ids {
			row, _ := lookup(id)
			if input.Action == "restore" {
				rewriteStoredReferences(e, row.Kind, objectName(row.Value), objectName(row.Original))
			}
			delete(e.Entries, id)
			removeRuntimeMove(e, id)
			kept := []string{}
			for _, v := range e.AddedOrder {
				if v != id {
					kept = append(kept, v)
				}
			}
			e.AddedOrder = kept
		}
	default:
		return fmt.Errorf("不支持的编辑操作")
	}
	// Remove exact no-op edits while retaining move-only snapshots for rebasing.
	for _, r := range baselineRows(base) {
		if entry, ok := e.Entries[r.ID]; ok && !entry.Added && !entry.Disabled && sameValue(entry.Value, r.Original) {
			moved := false
			for _, move := range e.Moves {
				if move.ID == r.ID {
					moved = true
				}
			}
			if !moved {
				delete(e.Entries, r.ID)
			}
		}
	}
	candidate := cloneValue(base).(map[string]interface{})
	if err := applyRuntimeEdits(candidate, *e); err != nil {
		return err
	}
	return validateRuntimeReferences(candidate)
}
