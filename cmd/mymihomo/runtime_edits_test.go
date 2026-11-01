package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const runtimeFixture = `proxies: []
proxy-groups:
  - name: Primary
    type: select
    proxies: [DIRECT, REJECT]
  - name: Parent
    type: select
    proxies: [Primary, DIRECT]
rule-providers:
  local:
    type: inline
    behavior: domain
    payload: ['+.example.com']
rules:
  - DOMAIN,original.example,Primary
  - RULE-SET,local,Primary
  - MATCH,DIRECT
`

func runtimeFixtureManager(t *testing.T) (*profileManager, ProfileState) {
	m, s := managerFixture(t)
	s.Profiles[0].Content = runtimeFixture
	s.GlobalMerge = "prepend-rules:\n  - DOMAIN,global.example,DIRECT\n"
	s.GlobalScript = "function main(c) { c.rules.unshift('DOMAIN,script.example,DIRECT'); return c; }"
	_ = m.save(s)
	return m, s
}
func runtimeAPICall(t *testing.T, m *profileManager, method string, input interface{}, expected int) map[string]interface{} {
	t.Helper()
	body, _ := json.Marshal(input)
	rec := httptest.NewRecorder()
	m.handleRuntimeEdits(rec, httptest.NewRequest(method, "/api/runtime-edits", strings.NewReader(string(body))))
	if rec.Code != expected {
		t.Fatalf("%s runtime API: %d %s", method, rec.Code, rec.Body.String())
	}
	var result map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	return result
}
func mutateAPI(t *testing.T, m *profileManager, action map[string]interface{}, expected int) {
	t.Helper()
	snapshot := runtimeAPICall(t, m, "GET", nil, 200)
	action["profile_id"] = snapshot["profile_id"]
	action["revision"] = snapshot["revision"]
	runtimeAPICall(t, m, "POST", action, expected)
}
func findRuntimeRow(t *testing.T, m *profileManager, kind, name string) map[string]interface{} {
	t.Helper()
	snapshot := runtimeAPICall(t, m, "GET", nil, 200)
	for _, v := range snapshot[kind].([]interface{}) {
		row := v.(map[string]interface{})
		if kind == "rules" {
			if strings.Contains(row["value"].(string), name) {
				return row
			}
		} else if row["value"].(map[string]interface{})["name"] == name {
			return row
		}
	}
	t.Fatalf("row not found %s %s", kind, name)
	return nil
}
func TestRuntimeLayerPersistenceRenameDisableAndRestore(t *testing.T) {
	m, s := runtimeFixtureManager(t)
	original := s.Profiles[0].Content
	row := findRuntimeRow(t, m, "groups", "Primary")
	mutateAPI(t, m, map[string]interface{}{"action": "edit", "id": row["id"], "yaml": "name: Auto\ntype: url-test\nproxies: [DIRECT, REJECT]\nurl: https://example.test/204\ninterval: 300\n"}, 200)
	loaded, _ := newProfileManager(m.confFile).read()
	if loaded.Profiles[0].Content != original || loaded.GlobalMerge != s.GlobalMerge || loaded.GlobalScript != s.GlobalScript {
		t.Fatal("raw source/extensions changed")
	}
	output, err := effectiveConfig(loaded, loaded.Profiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "original.example,Auto") || !strings.Contains(string(output), "script.example") {
		t.Fatal("wrong extension order or group reference rewrite")
	}
	rule := findRuntimeRow(t, m, "rules", "RULE-SET")
	provider := findRuntimeRow(t, m, "providers", "local")
	mutateAPI(t, m, map[string]interface{}{"action": "toggle", "id": provider["id"], "disabled": true}, 200)
	if findRuntimeRow(t, m, "rules", "RULE-SET")["effective_enabled"] != false {
		t.Fatal("disabled provider did not suppress rule")
	}
	cfg, _ := os.ReadFile(m.confFile)
	if strings.Contains(string(cfg), "RULE-SET,local") {
		t.Fatal("disabled rule still applied")
	}
	mutateAPI(t, m, map[string]interface{}{"action": "restore", "ids": []interface{}{provider["id"]}}, 200)
	if findRuntimeRow(t, m, "rules", "RULE-SET")["effective_enabled"] != true {
		t.Fatal("provider restore did not re-enable rule")
	}
	mutateAPI(t, m, map[string]interface{}{"action": "toggle", "id": rule["id"], "disabled": true}, 200)
	if findRuntimeRow(t, m, "rules", "RULE-SET")["status"] != "modified" {
		t.Fatal("disable not marked modified")
	}
	mutateAPI(t, m, map[string]interface{}{"action": "restore", "id": row["id"]}, 200)
	if findRuntimeRow(t, m, "groups", "Primary")["status"] != "unchanged" {
		t.Fatal("restore not baseline")
	}
}
func TestRuntimeUpdateRebasesAndMissingOriginalIsVisible(t *testing.T) {
	m, s := runtimeFixtureManager(t)
	row := findRuntimeRow(t, m, "rules", "original.example")
	mutateAPI(t, m, map[string]interface{}{"action": "edit", "id": row["id"], "yaml": "DOMAIN,custom.example,REJECT"}, 200)
	loaded, _ := m.read()
	loaded.Profiles[0].Content = strings.Replace(s.Profiles[0].Content, "  - DOMAIN,original.example,Primary\n", "  - DOMAIN,new.example,DIRECT\n", 1)
	if err := m.commit(loaded, true); err != nil {
		t.Fatal(err)
	}
	if findRuntimeRow(t, m, "rules", "custom.example")["missing"] != true {
		t.Fatal("orphan edit not visible")
	}
	if findRuntimeRow(t, m, "rules", "new.example")["status"] != "unchanged" {
		t.Fatal("upstream addition marked user-added")
	}
	mutateAPI(t, m, map[string]interface{}{"action": "restore", "id": row["id"]}, 200)
	snapshot := runtimeAPICall(t, m, "GET", nil, 200)
	for _, v := range snapshot["rules"].([]interface{}) {
		if strings.Contains(v.(map[string]interface{})["yaml"].(string), "custom.example") {
			t.Fatal("orphan restore retained edit")
		}
	}
}
func TestRuntimeBatchDeleteAndReferenceFailureRollback(t *testing.T) {
	m, _ := runtimeFixtureManager(t)
	mutateAPI(t, m, map[string]interface{}{"action": "create", "kind": "groups", "yaml": "name: Added\ntype: select\nproxies: [DIRECT]\n"}, 200)
	group := findRuntimeRow(t, m, "groups", "Added")
	mutateAPI(t, m, map[string]interface{}{"action": "create", "kind": "rules", "yaml": "DOMAIN,added.example,Added"}, 200)
	rule := findRuntimeRow(t, m, "rules", "added.example")
	before, _ := os.ReadFile(m.path)
	cfg, _ := os.ReadFile(m.confFile)
	mutateAPI(t, m, map[string]interface{}{"action": "delete", "ids": []interface{}{group["id"]}}, 400)
	after, _ := os.ReadFile(m.path)
	current, _ := os.ReadFile(m.confFile)
	if string(before) != string(after) || string(cfg) != string(current) {
		t.Fatal("invalid deletion changed runtime or store")
	}
	mutateAPI(t, m, map[string]interface{}{"action": "delete", "ids": []interface{}{group["id"], rule["id"]}}, 200)
	baseline := findRuntimeRow(t, m, "groups", "Primary")
	mutateAPI(t, m, map[string]interface{}{"action": "delete", "id": baseline["id"]}, 400)
	// Applying an invalid candidate cannot overwrite last-good state.
	m.apply = func([]byte) error { return fmt.Errorf("rejected") }
	before, _ = os.ReadFile(m.path)
	mutateAPI(t, m, map[string]interface{}{"action": "toggle", "id": findRuntimeRow(t, m, "rules", "MATCH")["id"], "disabled": true}, 400)
	after, _ = os.ReadFile(m.path)
	if string(before) != string(after) {
		t.Fatal("failed apply persisted edits")
	}
}
func TestRuntimeOrderRevisionAndProfileIsolation(t *testing.T) {
	m, _ := runtimeFixtureManager(t)
	original := findRuntimeRow(t, m, "rules", "original.example")
	script := findRuntimeRow(t, m, "rules", "script.example")
	mutateAPI(t, m, map[string]interface{}{"action": "move", "id": original["id"], "before": script["id"]}, 200)
	snapshot := runtimeAPICall(t, m, "GET", nil, 200)
	if snapshot["rules"].([]interface{})[0].(map[string]interface{})["id"] != original["id"] {
		t.Fatal("move did not reorder")
	}
	stale := map[string]interface{}{"profile_id": snapshot["profile_id"], "revision": snapshot["revision"], "action": "restore", "id": original["id"]}
	mutateAPI(t, m, map[string]interface{}{"action": "restore", "id": original["id"]}, 200)
	runtimeAPICall(t, m, "POST", stale, 409)
	state, _ := m.read()
	next := state.Profiles[0]
	next.ID = "other"
	next.RuntimeEdits = RuntimeEdits{}
	state.Profiles = append(state.Profiles, next)
	state.ActiveID = next.ID
	_ = m.save(state)
	runtimeAPICall(t, m, "POST", stale, 409)
	if findRuntimeRow(t, m, "rules", "original.example")["status"] != "unchanged" {
		t.Fatal("edits leaked to other profile")
	}
}
func TestRuntimeCyclesAndProviderRename(t *testing.T) {
	m, _ := runtimeFixtureManager(t)
	primary := findRuntimeRow(t, m, "groups", "Primary")
	mutateAPI(t, m, map[string]interface{}{"action": "edit", "id": primary["id"], "yaml": "name: Primary\ntype: select\nproxies: [Parent]"}, 400)
	provider := findRuntimeRow(t, m, "providers", "local")
	mutateAPI(t, m, map[string]interface{}{"action": "edit", "id": provider["id"], "yaml": "name: renamed\ntype: inline\nbehavior: domain\npayload: ['+.example.com']"}, 200)
	cfg, _ := os.ReadFile(m.confFile)
	var parsed map[string]interface{}
	_ = yaml.Unmarshal(cfg, &parsed)
	if !strings.Contains(string(cfg), "RULE-SET,renamed,Primary") {
		t.Fatal("rule set rename broke reference")
	}
	mutateAPI(t, m, map[string]interface{}{"action": "restore", "id": provider["id"]}, 200)
}

func TestRuntimeFieldRebaseFollowsNewUpstreamMembersAndRulePolicy(t *testing.T) {
	m, _ := runtimeFixtureManager(t)
	group := findRuntimeRow(t, m, "groups", "Primary")
	mutateAPI(t, m, map[string]interface{}{"action": "edit", "id": group["id"], "yaml": "name: Primary\ntype: url-test\nproxies: [DIRECT, REJECT]\nurl: https://example.test/204\ninterval: 300"}, 200)
	rule := findRuntimeRow(t, m, "rules", "original.example")
	mutateAPI(t, m, map[string]interface{}{"action": "toggle", "id": rule["id"], "disabled": true}, 200)
	state, _ := m.read()
	state.Profiles[0].Content = strings.ReplaceAll(state.Profiles[0].Content, "[DIRECT, REJECT]", "[DIRECT, REJECT, PASS]")
	state.Profiles[0].Content = strings.ReplaceAll(state.Profiles[0].Content, "DOMAIN,original.example,Primary", "DOMAIN,original.example,REJECT")
	if err := m.commit(state, true); err != nil {
		t.Fatal(err)
	}
	current := findRuntimeRow(t, m, "groups", "Primary")["value"].(map[string]interface{})
	if current["type"] != "url-test" || len(current["proxies"].([]interface{})) != 3 {
		t.Fatal("upstream members were frozen by unrelated type edit")
	}
	currentRule := findRuntimeRow(t, m, "rules", "original.example")
	if currentRule["id"] != rule["id"] || currentRule["missing"] != false || currentRule["value"] != "DOMAIN,original.example,REJECT" || currentRule["enabled"] != false {
		t.Fatal("disabled rule failed to rebase upstream policy")
	}
}

func TestRuntimeEditsPreserveLiveMode(t *testing.T) {
	m, _ := runtimeFixtureManager(t)
	m.readSettings = func() (map[string]interface{}, error) {
		return map[string]interface{}{"mode": "global", "log-level": "warning"}, nil
	}
	mutateAPI(t, m, map[string]interface{}{"action": "create", "kind": "rules", "yaml": "DOMAIN,preserve.example,DIRECT"}, 200)
	data, _ := os.ReadFile(m.confFile)
	var cfg map[string]interface{}
	_ = yaml.Unmarshal(data, &cfg)
	if cfg["mode"] != "global" || cfg["log-level"] != "warning" {
		t.Fatal("runtime editor reset unrelated live settings")
	}
}

func TestMissingGroupRetainsLastSuccessfulRebasedVersionAndReferences(t *testing.T) {
	m, _ := runtimeFixtureManager(t)
	group := findRuntimeRow(t, m, "groups", "Primary")
	mutateAPI(t, m, map[string]interface{}{"action": "edit", "id": group["id"], "yaml": "name: MyAuto\ntype: url-test\nproxies: [DIRECT, REJECT]\nurl: https://example.test/204\ninterval: 300"}, 200)
	state, _ := m.read()
	state.Profiles[0].Content = strings.ReplaceAll(state.Profiles[0].Content, "[DIRECT, REJECT]", "[DIRECT, REJECT, PASS]")
	if err := m.commit(state, true); err != nil {
		t.Fatal(err)
	}
	state, _ = m.read()
	state.Profiles[0].Content = strings.Replace(state.Profiles[0].Content, "  - name: Primary\n    type: select\n    proxies: [DIRECT, REJECT, PASS]\n", "", 1)
	if err := m.commit(state, true); err != nil {
		t.Fatal(err)
	}
	missing := findRuntimeRow(t, m, "groups", "MyAuto")
	value := missing["value"].(map[string]interface{})
	if missing["missing"] != true || len(value["proxies"].([]interface{})) != 3 {
		t.Fatal("missing item lost last successful upstream additions")
	}
	before, _ := os.ReadFile(m.path)
	mutateAPI(t, m, map[string]interface{}{"action": "restore", "id": group["id"]}, 400)
	after, _ := os.ReadFile(m.path)
	if string(before) != string(after) {
		t.Fatal("restore deleted a referenced missing group")
	}
	mutateAPI(t, m, map[string]interface{}{"action": "edit", "id": group["id"], "yaml": "name: MyAuto\ntype: select\nproxies: [DIRECT, REJECT, PASS]"}, 200)
	if findRuntimeRow(t, m, "groups", "MyAuto")["value"].(map[string]interface{})["type"] != "select" {
		t.Fatal("missing item edit ignored")
	}
	returned, _ := m.read()
	returned.Profiles[0].Content = strings.Replace(returned.Profiles[0].Content, "  - name: Parent", "  - name: Primary\n    type: select\n    proxies: [DIRECT]\n  - name: Parent", 1)
	if err := m.commit(returned, true); err != nil {
		t.Fatal(err)
	}
	restored := findRuntimeRow(t, m, "groups", "MyAuto")
	if restored["missing"] != false || len(restored["value"].(map[string]interface{})["proxies"].([]interface{})) != 1 {
		t.Fatal("returning source failed to resume field-level rebasing")
	}
}

func TestRemovedEditedRuleKeepsSurvivingOrderAnchor(t *testing.T) {
	m, _ := runtimeFixtureManager(t)
	original := findRuntimeRow(t, m, "rules", "original.example")
	mutateAPI(t, m, map[string]interface{}{"action": "edit", "id": original["id"], "yaml": "DOMAIN,custom.example,DIRECT"}, 200)
	state, _ := m.read()
	state.Profiles[0].Content = strings.Replace(state.Profiles[0].Content, "  - DOMAIN,original.example,Primary\n", "", 1)
	if err := m.commit(state, true); err != nil {
		t.Fatal(err)
	}
	data := runtimeAPICall(t, m, "GET", nil, 200)["rules"].([]interface{})
	previous := ""
	for _, v := range data {
		r := v.(map[string]interface{})
		if strings.HasPrefix(r["value"].(string), "RULE-SET,") && previous != "DOMAIN,custom.example,DIRECT" {
			t.Fatal("missing edited rule moved behind its surviving successor")
		}
		previous = r["value"].(string)
	}
}
func TestDuplicateConditionRemovalDoesNotRetargetAnotherRule(t *testing.T) {
	m, s := runtimeFixtureManager(t)
	s.Profiles[0].Content = strings.Replace(s.Profiles[0].Content, "  - DOMAIN,original.example,Primary\n", "  - DOMAIN,same.example,Primary\n  - DOMAIN,same.example,REJECT\n", 1)
	_ = m.save(s)
	first := findRuntimeRow(t, m, "rules", "DOMAIN,same.example,Primary")
	mutateAPI(t, m, map[string]interface{}{"action": "edit", "id": first["id"], "yaml": "DOMAIN,same.example,DIRECT"}, 200)
	state, _ := m.read()
	state.Profiles[0].Content = strings.Replace(state.Profiles[0].Content, "  - DOMAIN,same.example,Primary\n", "", 1)
	if err := m.commit(state, true); err != nil {
		t.Fatal(err)
	}
	if findRuntimeRow(t, m, "rules", "DOMAIN,same.example,DIRECT")["missing"] != true {
		t.Fatal("edit attached to a different rule")
	}
	if findRuntimeRow(t, m, "rules", "DOMAIN,same.example,REJECT")["status"] != "unchanged" {
		t.Fatal("remaining upstream rule was overwritten")
	}
}

func TestRuntimeRejectsNonSerializableYAMLWithoutChangingState(t *testing.T) {
	m, _ := runtimeFixtureManager(t)
	before, _ := os.ReadFile(m.path)
	mutateAPI(t, m, map[string]interface{}{"action": "create", "kind": "providers", "yaml": "name: bad\ntype: inline\nbehavior: domain\npayload: ['+.example.com']\ninterval: .nan"}, 400)
	after, _ := os.ReadFile(m.path)
	if string(before) != string(after) {
		t.Fatal("invalid YAML changed store")
	}
}
