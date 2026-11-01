package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Stored separately from downloaded content and extension code, per profile.
// Keeping this in the profile transaction makes save/apply/rollback atomic.
type RuntimeEdits struct {
	Entries    map[string]RuntimeEdit `json:"entries,omitempty"`
	AddedOrder []string               `json:"added_order,omitempty"`
	Moves      []RuntimeMove          `json:"moves,omitempty"`
}
type RuntimeEdit struct {
	LastBase     interface{} `json:"last_base,omitempty"`
	LastBefore   []string    `json:"last_before,omitempty"`
	LastPosition int         `json:"last_position,omitempty"`
	LastValue    interface{} `json:"last_value,omitempty"`
	Kind         string      `json:"kind"`
	Added        bool        `json:"added"`
	Value        interface{} `json:"value"`
	Base         interface{} `json:"base,omitempty"`
	Disabled     bool        `json:"disabled,omitempty"`
}
type RuntimeMove struct {
	ID     string `json:"id"`
	Before string `json:"before"`
}
type RuntimeRow struct {
	SourceName       string      `json:"-"`
	ID               string      `json:"id"`
	Kind             string      `json:"kind"`
	Status           string      `json:"status"`
	Enabled          bool        `json:"enabled"`
	EffectiveEnabled bool        `json:"effective_enabled"`
	Missing          bool        `json:"missing"`
	Moved            bool        `json:"moved"`
	BlockedBy        []string    `json:"blocked_by,omitempty"`
	Value            interface{} `json:"value"`
	Original         interface{} `json:"original"`
	YAML             string      `json:"yaml"`
	OriginalYAML     string      `json:"original_yaml"`
}

func cloneValue(value interface{}) interface{} {
	b, _ := json.Marshal(value)
	var result interface{}
	_ = json.Unmarshal(b, &result)
	return result
}
func sameValue(a, b interface{}) bool { return reflect.DeepEqual(cloneValue(a), cloneValue(b)) }
func runtimeID(kind, key string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + key))
	return kind + "-" + hex.EncodeToString(sum[:12])
}
func objectName(v interface{}) string {
	m, _ := v.(map[string]interface{})
	name, _ := m["name"].(string)
	return name
}
func yamlText(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := yaml.Marshal(v)
	return string(b)
}
func validRuntimeKind(kind string) bool {
	return kind == "groups" || kind == "rules" || kind == "providers"
}
func baselineRows(c map[string]interface{}) []RuntimeRow {
	rows := []RuntimeRow{}
	add := func(kind, key string, value interface{}) {
		rows = append(rows, RuntimeRow{ID: runtimeID(kind, key), Kind: kind, Status: "unchanged", Enabled: true, EffectiveEnabled: true, Value: cloneValue(value), Original: cloneValue(value)})
	}
	groups, _ := c["proxy-groups"].([]interface{})
	for _, g := range groups {
		add("groups", objectName(g), g)
	}
	providers, _ := c["rule-providers"].(map[string]interface{})
	names := []string{}
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		v, _ := cloneValue(providers[name]).(map[string]interface{})
		if v == nil {
			v = map[string]interface{}{}
		}
		v["name"] = name
		add("providers", name, v)
	}

	rules, _ := c["rules"].([]interface{})
	condition := func(value interface{}) string {
		parts := ruleParts(fmt.Sprint(value))
		key := parts[0]
		if key != "MATCH" && len(parts) > 1 {
			key += "," + parts[1]
		}
		return key
	}
	counts := map[string]int{}
	for _, value := range rules {
		counts[condition(value)]++
	}
	occurrences := map[string]int{}
	for _, value := range rules {
		rule, _ := value.(string)
		key := condition(value)
		if counts[key] > 1 {
			key = fmt.Sprintf("%s\x00duplicates:%d\x00%s", key, counts[key], strings.TrimSpace(rule))
		}
		occurrences[key]++
		add("rules", fmt.Sprintf("%s\x00%d", key, occurrences[key]), rule)
	}

	return rows
}
func rowIndex(rows []RuntimeRow, id string) int {
	for i := range rows {
		if rows[i].ID == id {
			return i
		}
	}
	return -1
}
func beforeCatchAll(rows []RuntimeRow) int {
	for i, r := range rows {
		if r.Kind == "rules" && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(fmt.Sprint(r.Value))), "MATCH,") {
			return i
		}
	}
	return len(rows)
}
func insertRow(rows []RuntimeRow, index int, row RuntimeRow) []RuntimeRow {
	rows = append(rows, RuntimeRow{})
	copy(rows[index+1:], rows[index:])
	rows[index] = row
	return rows
}
func runtimeRows(c map[string]interface{}, edits RuntimeEdits) ([]RuntimeRow, error) {
	rows := baselineRows(c)
	known := map[string]bool{}
	for i := range rows {
		r := &rows[i]
		known[r.ID] = true
		if e, ok := edits.Entries[r.ID]; ok {
			if e.Kind != r.Kind || e.Added {
				return nil, fmt.Errorf("运行时编辑标识冲突")
			}
			r.Value = rebaseRuntimeValue(r.Original, e.Base, e.Value, r.Kind)
			r.Enabled = !e.Disabled
			if e.Disabled || !sameValue(r.Value, r.Original) {
				r.Status = "modified"
			}
		}
	}
	// An upstream-deleted edited item is retained visibly until explicitly restored.
	missing := []string{}
	for id, e := range edits.Entries {
		if !known[id] && !e.Added {
			missing = append(missing, id)
		}
	}
	sort.SliceStable(missing, func(i, j int) bool {
		a, b := edits.Entries[missing[i]].LastPosition, edits.Entries[missing[j]].LastPosition
		if a == b {
			return missing[i] < missing[j]
		}
		return a < b
	})
	added := append(missing, edits.AddedOrder...)
	for _, id := range added {
		if known[id] {
			continue
		}
		e, ok := edits.Entries[id]
		if !ok {
			continue
		}
		known[id] = true
		status := "modified"
		if e.Added {
			status = "added"
		}
		value := e.Value
		if !e.Added && e.LastValue != nil {
			value = e.LastValue
		}
		r := RuntimeRow{ID: id, Kind: e.Kind, Status: status, Enabled: !e.Disabled, Missing: !e.Added, Value: cloneValue(value), SourceName: objectName(e.Base)}
		if r.Kind == "rules" {
			at := beforeCatchAll(rows)
			if r.Missing {
				for _, id := range e.LastBefore {
					if index := rowIndex(rows, id); index >= 0 && rows[index].Kind == "rules" {
						at = index
						break
					}
				}
			}
			rows = insertRow(rows, at, r)
		} else {
			rows = append(rows, r)
		}
	}
	for _, move := range edits.Moves {
		index := rowIndex(rows, move.ID)
		if index < 0 {
			continue
		}
		r := rows[index]
		if r.Kind != "rules" {
			return nil, fmt.Errorf("只有规则支持排序")
		}
		rows = append(rows[:index], rows[index+1:]...)
		at := len(rows)
		if move.Before != "" {
			at = rowIndex(rows, move.Before)
			if at < 0 {
				at = beforeCatchAll(rows)
				for _, id := range edits.Entries[r.ID].LastBefore {
					if index := rowIndex(rows, id); index >= 0 && rows[index].Kind == "rules" {
						at = index
						break
					}
				}
			} else if rows[at].Kind != "rules" {
				return nil, fmt.Errorf("规则位置无效")
			}
		}
		r.Moved = true
		if r.Status == "unchanged" {
			r.Status = "modified"
		}
		rows = insertRow(rows, at, r)
	}
	aliases := runtimeAliases(rows)
	disabled := map[string]bool{}
	for _, r := range rows {
		if r.Kind == "providers" && !r.Enabled {
			disabled[objectName(r.Value)] = true
		}
	}
	for i := range rows {
		r := &rows[i]
		r.Value = rewriteReferences(r.Kind, r.Value, aliases)
		r.EffectiveEnabled = r.Enabled
		if r.Kind == "rules" {
			for _, name := range ruleProviderRefs(fmt.Sprint(r.Value)) {
				if disabled[name] {
					r.BlockedBy = append(r.BlockedBy, name)
					r.EffectiveEnabled = false
				}
			}
		}
		r.YAML = yamlText(r.Value)
		r.OriginalYAML = yamlText(r.Original)
	}
	return rows, nil
}

type nameAliases struct {
	Groups    map[string]string
	Providers map[string]string
}

func runtimeAliases(rows []RuntimeRow) nameAliases {
	a := nameAliases{Groups: map[string]string{}, Providers: map[string]string{}}
	for _, r := range rows {
		old, name := objectName(r.Original), objectName(r.Value)
		if old == "" {
			old = r.SourceName
		}
		if old != "" && old != name {
			if r.Kind == "groups" {
				a.Groups[old] = name
			}
			if r.Kind == "providers" {
				a.Providers[old] = name
			}
		}
	}
	return a
}

var ruleSetPattern = regexp.MustCompile(`(^|[,(])\s*RULE-SET\s*,\s*([^,()]+)`)

func ruleProviderRefs(rule string) []string {
	out := []string{}
	for _, m := range ruleSetPattern.FindAllStringSubmatch(rule, -1) {
		out = append(out, strings.TrimSpace(m[2]))
	}
	return out
}
func ruleParts(rule string) []string {
	parts := []string{}
	start, depth := 0, 0
	for i, c := range rule {
		if c == '(' {
			depth++
		}
		if c == ')' {
			depth--
		}
		if c == ',' && depth == 0 {
			parts = append(parts, strings.TrimSpace(rule[start:i]))
			start = i + 1
		}
	}
	return append(parts, strings.TrimSpace(rule[start:]))
}
func rewriteRule(rule string, a nameAliases) string {
	rule = ruleSetPattern.ReplaceAllStringFunc(rule, func(fragment string) string {
		m := ruleSetPattern.FindStringSubmatch(fragment)
		if name, ok := a.Providers[strings.TrimSpace(m[2])]; ok {
			return strings.TrimSuffix(fragment, m[2]) + name
		}
		return fragment
	})
	parts := ruleParts(rule)
	if len(parts) < 2 {
		return rule
	}
	index := 2
	if strings.EqualFold(parts[0], "MATCH") {
		index = 1
	}
	if parts[0] == "SUB-RULE" {
		return rule
	}
	if index < len(parts) {
		if name, ok := a.Groups[parts[index]]; ok {
			parts[index] = name
			return strings.Join(parts, ",")
		}
	}
	return rule
}
func rewriteReferences(kind string, value interface{}, a nameAliases) interface{} {
	if kind == "rules" {
		return rewriteRule(fmt.Sprint(value), a)
	}
	m, _ := cloneValue(value).(map[string]interface{})
	if m == nil {
		return value
	}
	if kind == "groups" {
		if members, ok := m["proxies"].([]interface{}); ok {
			for i, v := range members {
				if name, ok := a.Groups[fmt.Sprint(v)]; ok {
					members[i] = name
				}
			}
		}
	}
	if proxy, ok := m["proxy"].(string); ok {
		if name, ok := a.Groups[proxy]; ok {
			m["proxy"] = name
		}
	}
	return m
}
func applyRuntimeEdits(c map[string]interface{}, edits RuntimeEdits) error {
	if len(edits.Entries) == 0 && len(edits.Moves) == 0 {
		return nil
	}
	rows, err := runtimeRows(c, edits)
	if err != nil {
		return err
	}
	groups := []interface{}{}
	rules := []interface{}{}
	providers := map[string]interface{}{}
	names := map[string]bool{}
	disabled := map[string]bool{}
	for _, r := range rows {
		if r.Kind == "groups" {
			name := objectName(r.Value)
			if names["g:"+name] {
				return fmt.Errorf("代理组名称冲突：%s", name)
			}
			names["g:"+name] = true
			groups = append(groups, r.Value)
		}
		if r.Kind == "providers" {
			name := objectName(r.Value)
			if names["p:"+name] {
				return fmt.Errorf("规则集名称冲突：%s", name)
			}
			names["p:"+name] = true
			if !r.Enabled {
				disabled[name] = true
				continue
			}
			v, ok := cloneValue(r.Value).(map[string]interface{})
			if !ok {
				return fmt.Errorf("规则集配置必须是对象")
			}
			delete(v, "name")
			providers[name] = v
		}
		if r.Kind == "rules" && r.EffectiveEnabled {
			rules = append(rules, r.Value)
		}
	}
	a := runtimeAliases(rows)
	if subs, ok := c["sub-rules"].(map[string]interface{}); ok {
		for key, value := range subs {
			if list, ok := value.([]interface{}); ok {
				kept := []interface{}{}
				for _, v := range list {
					rule := rewriteRule(fmt.Sprint(v), a)
					blocked := false
					for _, name := range ruleProviderRefs(rule) {
						if disabled[name] {
							blocked = true
						}
					}
					if !blocked {
						kept = append(kept, rule)
					}
				}
				subs[key] = kept
			}
		}
	}
	if sources, ok := c["proxy-providers"].(map[string]interface{}); ok {
		for key, v := range sources {
			sources[key] = rewriteReferences("providers", v, a)
		}
	}
	c["proxy-groups"], c["rules"], c["rule-providers"] = groups, rules, providers
	return validateRuntimeReferences(c)
}
func validateRuntimeReferences(c map[string]interface{}) error {
	names := map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "COMPATIBLE": true, "GLOBAL": true}
	proxies, _ := c["proxies"].([]interface{})
	for _, p := range proxies {
		names[objectName(p)] = true
	}
	groups, _ := c["proxy-groups"].([]interface{})
	groupNames := map[string]bool{}
	for _, v := range groups {
		name := objectName(v)
		if name == "" || names[name] || groupNames[name] {
			return fmt.Errorf("代理组名称为空或重复：%s", name)
		}
		groupNames[name] = true
	}
	for name := range groupNames {
		names[name] = true
	}
	proxyProviders, _ := c["proxy-providers"].(map[string]interface{})
	ruleProviders, _ := c["rule-providers"].(map[string]interface{})
	graph := map[string][]string{}
	for _, v := range groups {
		g, _ := v.(map[string]interface{})
		name := objectName(g)
		for _, field := range []string{"proxies", "use"} {
			if members, ok := g[field].([]interface{}); ok {
				for _, item := range members {
					ref := fmt.Sprint(item)
					if field == "proxies" {
						if !names[ref] {
							return fmt.Errorf("代理组 %s 引用了不存在的节点/组 %s", name, ref)
						}
						if groupNames[ref] {
							graph[name] = append(graph[name], ref)
						}
					} else if _, ok := proxyProviders[ref]; !ok {
						return fmt.Errorf("代理组 %s 引用了不存在的代理提供者 %s", name, ref)
					}
				}
			}
		}
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if visiting[name] {
			return fmt.Errorf("代理组存在循环引用：%s", name)
		}
		if visited[name] {
			return nil
		}
		visiting[name] = true
		for _, next := range graph[name] {
			if err := visit(next); err != nil {
				return err
			}
		}
		visiting[name] = false
		visited[name] = true
		return nil
	}
	for name := range graph {
		if err := visit(name); err != nil {
			return err
		}
	}
	validateRules := func(value interface{}) error {
		list, _ := value.([]interface{})
		for _, v := range list {
			rule, ok := v.(string)
			if !ok {
				return fmt.Errorf("规则必须是字符串")
			}
			for _, ref := range ruleProviderRefs(rule) {
				if _, ok := ruleProviders[ref]; !ok {
					return fmt.Errorf("规则引用了不存在的规则集 %s", ref)
				}
			}
			parts := ruleParts(rule)
			at := 2
			if parts[0] == "MATCH" {
				at = 1
			}
			if parts[0] != "SUB-RULE" && len(parts) > at && !names[parts[at]] {
				return fmt.Errorf("规则引用了不存在的代理组/策略 %s", parts[at])
			}
		}
		return nil
	}
	if err := validateRules(c["rules"]); err != nil {
		return err
	}
	if subs, ok := c["sub-rules"].(map[string]interface{}); ok {
		for _, v := range subs {
			if err := validateRules(v); err != nil {
				return err
			}
		}
	}
	return nil
}

// Reapply only fields the user changed. New upstream members/URLs/defaults keep
// flowing through unless that specific field was explicitly overridden.
func rebaseRuntimeValue(current, base, edited interface{}, kind string) interface{} {
	if base == nil {
		return cloneValue(edited)
	}
	if sameValue(base, edited) {
		return cloneValue(current)
	}
	if kind == "rules" {
		b, e, c := ruleParts(fmt.Sprint(base)), ruleParts(fmt.Sprint(edited)), ruleParts(fmt.Sprint(current))
		at := 2
		if b[0] == "MATCH" {
			at = 1
		}
		if b[0] != e[0] || len(b) <= at || len(e) <= at || len(c) <= at {
			return cloneValue(edited)
		}
		for i := 0; i <= at; i++ {
			if b[i] != e[i] {
				c[i] = e[i]
			}
		}
		if !reflect.DeepEqual(b[at+1:], e[at+1:]) {
			c = append(c[:at+1], e[at+1:]...)
		}
		return strings.Join(c, ",")
	}
	b, bok := cloneValue(base).(map[string]interface{})
	e, eok := cloneValue(edited).(map[string]interface{})
	c, cok := cloneValue(current).(map[string]interface{})
	if !bok || !eok || !cok {
		return cloneValue(edited)
	}
	for key, bv := range b {
		ev, exists := e[key]
		if !exists {
			delete(c, key)
		} else if !sameValue(bv, ev) {
			c[key] = rebaseRuntimeValue(c[key], bv, ev, "")
		}
	}
	for key, ev := range e {
		if _, exists := b[key]; !exists {
			c[key] = ev
		}
	}
	return c
}

// Called only for a validated candidate that is about to be committed. Failed
// updates never replace this snapshot of the last successfully applied item.
func captureRuntimeEdits(s ProfileState, p *Profile) error {
	if len(p.RuntimeEdits.Entries) == 0 {
		return nil
	}
	base, err := baselineConfig(s, *p)
	if err != nil {
		return err
	}
	rows, err := runtimeRows(base, p.RuntimeEdits)
	if err != nil {
		return err
	}
	position := 0
	for rowIndex, row := range rows {
		if row.Kind == "rules" {
			position++
		}
		if entry, ok := p.RuntimeEdits.Entries[row.ID]; ok && !entry.Added {
			if row.Kind == "rules" && !row.Missing {
				entry.LastPosition = position
				entry.LastBefore = nil
				for _, next := range rows[rowIndex+1:] {
					if next.Kind == "rules" {
						entry.LastBefore = append(entry.LastBefore, next.ID)
					}
					if len(entry.LastBefore) >= 16 {
						break
					}
				}
			}
			entry.LastValue = cloneValue(row.Value)
			if !row.Missing {
				entry.LastBase = cloneValue(row.Original)
			}
			p.RuntimeEdits.Entries[row.ID] = entry
		}
	}
	return nil
}
func (m *profileManager) commitProfile(s ProfileState, id string) error {
	if s.ActiveID == id {
		return m.commit(s, true)
	}
	index := profileIndex(s, id)
	if index < 0 {
		return fmt.Errorf("订阅不存在")
	}
	if err := captureRuntimeEdits(s, &s.Profiles[index]); err != nil {
		return err
	}
	return m.save(s)
}
