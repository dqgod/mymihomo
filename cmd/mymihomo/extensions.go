package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
	"gopkg.in/yaml.v3"
)

func parseConfig(data string) (map[string]interface{}, error) {
	var config map[string]interface{}
	if err := yaml.Unmarshal([]byte(data), &config); err != nil || config == nil {
		return nil, fmt.Errorf("配置必须是有效的 Clash/Mihomo YAML 对象")
	}
	return config, nil
}

// Mapping values merge recursively; ordinary arrays replace. Explicit prepend-/append-
// keys provide predictable list editing. Prepending rules keeps them ahead of MATCH.
func mergeConfig(target, patch map[string]interface{}) error {
	for key, value := range patch {
		if strings.HasPrefix(key, "prepend-") || strings.HasPrefix(key, "append-") {
			parts := strings.SplitN(key, "-", 2)
			items, ok := value.([]interface{})
			if !ok {
				return fmt.Errorf("%s 必须是列表", key)
			}
			existing, _ := target[parts[1]].([]interface{})
			if parts[0] == "prepend" {
				target[parts[1]] = append(items, existing...)
			} else {
				target[parts[1]] = append(existing, items...)
			}
			continue
		}
		if value == nil {
			delete(target, key)
			continue
		}
		if child, ok := value.(map[string]interface{}); ok {
			existing, ok := target[key].(map[string]interface{})
			if !ok {
				existing = map[string]interface{}{}
			}
			if err := mergeConfig(existing, child); err != nil {
				return err
			}
			target[key] = existing
		} else {
			target[key] = value
		}
	}
	return nil
}

func applyExtensions(config map[string]interface{}, merge, script, name string) (map[string]interface{}, error) {
	if strings.TrimSpace(merge) != "" {
		patch, err := parseConfig(merge)
		if err != nil {
			return nil, fmt.Errorf("覆写 YAML: %w", err)
		}
		if err = mergeConfig(config, patch); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(script) == "" {
		return config, nil
	}
	if len(script) > 256*1024 {
		return nil, fmt.Errorf("扩展脚本不能超过 256 KiB")
	}
	vm := goja.New()
	// Pure configuration transforms: no filesystem, process, network, require or fetch.
	_ = vm.Set("console", map[string]interface{}{"log": func(...interface{}) {}, "warn": func(...interface{}) {}, "error": func(...interface{}) {}})
	timer := time.AfterFunc(2*time.Second, func() { vm.Interrupt("脚本执行超过 2 秒") })
	defer timer.Stop()
	if _, err := vm.RunString(script); err != nil {
		return nil, fmt.Errorf("扩展脚本: %w", err)
	}
	main, ok := goja.AssertFunction(vm.Get("main"))
	if !ok {
		return nil, fmt.Errorf("脚本需要定义 function main(config, profileName)，并返回配置对象")
	}
	// JSON conversion gives JavaScript ordinary objects/arrays, not reflected Go maps.
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("配置无法转换为 JavaScript 对象")
	}
	value, err := vm.RunString("(" + string(encoded) + ")")
	if err != nil {
		return nil, err
	}
	result, err := main(goja.Undefined(), value, vm.ToValue(name))
	if err != nil {
		return nil, fmt.Errorf("扩展脚本: %w", err)
	}
	if goja.IsUndefined(result) || goja.IsNull(result) {
		return nil, fmt.Errorf("扩展脚本必须返回配置对象")
	}
	encoded, err = json.Marshal(result.Export())
	if err != nil {
		return nil, fmt.Errorf("脚本返回了无法序列化的对象")
	}
	var output map[string]interface{}
	if err = json.Unmarshal(encoded, &output); err != nil || output == nil {
		return nil, fmt.Errorf("脚本必须返回配置对象")
	}
	return output, nil
}
