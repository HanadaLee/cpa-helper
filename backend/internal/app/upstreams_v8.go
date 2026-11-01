package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"
)

const upstreamV8Metadata = "_cpa_v8"

type upstreamV8Reference struct {
	Group int `json:"group"`
	Key   int `json:"key"`
}

type upstreamV8Snapshot struct {
	Groups   map[string][]map[string]any
	Revision string
}

func upstreamV8Family(section string) string { return strings.TrimSuffix(section, "-api-key") }

func upstreamObjectCopy(source map[string]any) map[string]any {
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func upstreamV8Keys(group map[string]any) ([]map[string]any, error) {
	encoded, err := json.Marshal(group["keys"])
	if err != nil {
		return nil, err
	}
	var keys []map[string]any
	if err := json.Unmarshal(encoded, &keys); err != nil {
		return nil, validationError("CPA V8 上游密钥分组格式无效")
	}
	for _, key := range keys {
		if key == nil {
			return nil, validationError("CPA V8 上游密钥分组格式无效")
		}
	}
	return keys, nil
}

func upstreamV8Effective(group, key map[string]any) map[string]any {
	item := upstreamObjectCopy(group)
	delete(item, "name")
	delete(item, "keys")
	for name, value := range key {
		if value != nil { // null inherits, whereas false, 0 and [] override.
			item[name] = value
		}
	}
	if index, ok := item["auth_index"]; ok {
		item["auth-index"] = index
		delete(item, "auth_index")
	}
	return item
}

func upstreamV8Items(groups []map[string]any, section string) ([]any, error) {
	items := []any{}
	for groupIndex, group := range groups {
		keys, err := upstreamV8Keys(group)
		if err != nil {
			return nil, err
		}
		if section == "openai-compatibility" {
			item := upstreamObjectCopy(group)
			delete(item, "keys")
			item["api-key-entries"] = keys
			item[upstreamV8Metadata] = upstreamV8Reference{Group: groupIndex, Key: -1}
			items = append(items, item)
			continue
		}
		for keyIndex, key := range keys {
			item := upstreamV8Effective(group, key)
			item[upstreamV8Metadata] = upstreamV8Reference{Group: groupIndex, Key: keyIndex}
			items = append(items, item)
		}
	}
	return items, nil
}

func (a *App) loadUpstreamsV8(r *http.Request, cfg AppConfig) (*upstreamV8Snapshot, error) {
	response, payload, err := a.keeperRequest(r.Context(), cfg, http.MethodGet,
		"/v8/management/config/api-keys", nil, nil, time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second)
	if response != nil && response.StatusCode == http.StatusNotFound {
		if strongCPAConfigRevision(response.Header.Get("ETag")) {
			return &upstreamV8Snapshot{Groups: map[string][]map[string]any{}, Revision: response.Header.Get("ETag")}, nil
		}
		return nil, nil // Only a missing V8 route permits the old Core contract.
	}
	if err != nil {
		return nil, err
	}
	if !strongCPAConfigRevision(response.Header.Get("ETag")) {
		return nil, validationError("CPA V8 未返回有效配置版本，请升级 CPA Core")
	}
	snapshot := &upstreamV8Snapshot{Revision: response.Header.Get("ETag")}
	if err := json.Unmarshal(payload, &snapshot.Groups); err != nil {
		return nil, validationError("CPA V8 上游配置响应格式无效")
	}
	return snapshot, nil
}

func clearUpstreamReadOnly(item map[string]any) {
	delete(item, upstreamV8Metadata)
	delete(item, "auth_index")
	delete(item, "auth-index")
}

func upstreamSharedField(field string) bool {
	switch field {
	case "priority", "prefix", "proxy-url", "headers", "models", "excluded-models", "disable-cooling", "request-retry", "request-scoped-errors":
		return true
	}
	return false
}

// An explicit empty override clears an inherited group value. A null would
// re-inherit it and silently undo the user's edit in the flattened form.
func emptyUpstreamOverride(value any) any {
	switch value.(type) {
	case []any, []map[string]any:
		return []any{}
	case map[string]any:
		return map[string]any{}
	case bool:
		return false
	case float64, int:
		return 0
	default:
		return ""
	}
}

func upstreamV8GroupName(groups []map[string]any, base string) string {
	used := map[string]bool{}
	for _, group := range groups {
		if name, ok := group["name"].(string); ok {
			used[name] = true
		}
	}
	name := base
	for suffix := 2; used[name]; suffix++ {
		name = fmt.Sprintf("%s-%d", base, suffix)
	}
	return name
}

func rebuildUpstreamsV8(groups []map[string]any, items []any, section string) ([]map[string]any, error) {
	result := make([]map[string]any, len(groups))
	originalKeys := make([][]map[string]any, len(groups))
	for i, group := range groups {
		var err error
		originalKeys[i], err = upstreamV8Keys(group)
		if err != nil {
			return nil, err
		}
		result[i] = upstreamObjectCopy(group)
		clearUpstreamReadOnly(result[i])
		result[i]["keys"] = []map[string]any{}
	}
	seen := map[upstreamV8Reference]bool{}
	for _, raw := range items {
		object, ok := raw.(map[string]any)
		if !ok || object == nil {
			return nil, validationError("上游配置必须是 JSON 对象数组")
		}
		item := upstreamObjectCopy(object)
		metadata, exists := item[upstreamV8Metadata]
		clearUpstreamReadOnly(item)
		if !exists {
			group := map[string]any{"name": upstreamV8GroupName(result, fmt.Sprintf("cpa-helper-%s-%d", upstreamV8Family(section), len(result)+1))}
			if section == "openai-compatibility" {
				group = item
				group["keys"] = item["api-key-entries"]
				delete(group, "api-key-entries")
			} else {
				for name, value := range item {
					if name == "base-url" || upstreamSharedField(name) {
						group[name] = value
						delete(item, name)
					}
				}
				group["keys"] = []map[string]any{item}
			}
			result = append(result, group)
			continue
		}
		var ref upstreamV8Reference
		encoded, err := json.Marshal(metadata)
		if err != nil || json.Unmarshal(encoded, &ref) != nil || ref.Group < 0 || ref.Group >= len(groups) || seen[ref] {
			return nil, validationError("CPA V8 上游引用无效，请重新加载")
		}
		seen[ref] = true
		if section == "openai-compatibility" {
			if ref.Key != -1 {
				return nil, validationError("CPA V8 上游引用无效，请重新加载")
			}
			item["keys"] = item["api-key-entries"]
			delete(item, "api-key-entries")
			result[ref.Group] = item
			continue
		}
		if ref.Key < 0 || ref.Key >= len(originalKeys[ref.Group]) {
			return nil, validationError("CPA V8 上游引用无效，请重新加载")
		}
		group := groups[ref.Group]
		key := upstreamObjectCopy(originalKeys[ref.Group][ref.Key])
		before := upstreamV8Effective(group, key)
		clearUpstreamReadOnly(before)
		clearUpstreamReadOnly(key)
		for name, value := range before {
			if name == "base-url" {
				continue
			}
			if _, kept := item[name]; !kept {
				delete(key, name)
				if upstreamSharedField(name) && group[name] != nil {
					key[name] = emptyUpstreamOverride(value)
				}
			}
		}
		for name, value := range item {
			if name != "base-url" && !reflect.DeepEqual(value, before[name]) {
				key[name] = value
			}
		}
		if !reflect.DeepEqual(item["base-url"], before["base-url"]) && len(originalKeys[ref.Group]) > 1 {
			// Endpoint belongs to the group. Move just this credential to a new
			// group rather than changing the endpoint for its untouched siblings.
			split := upstreamObjectCopy(group)
			clearUpstreamReadOnly(split)
			split["name"] = upstreamV8GroupName(result, fmt.Sprintf("%v-key-%d", group["name"], ref.Key+1))
			split["base-url"] = item["base-url"]
			split["keys"] = []map[string]any{key}
			result = append(result, split)
		} else {
			if !reflect.DeepEqual(item["base-url"], before["base-url"]) {
				result[ref.Group]["base-url"] = item["base-url"]
			}
			result[ref.Group]["keys"] = append(result[ref.Group]["keys"].([]map[string]any), key)
		}
	}
	out := []map[string]any{}
	for i, group := range result {
		keys, err := upstreamV8Keys(group)
		if err != nil {
			return nil, err
		}
		// Preserve originally empty/native keyless groups unless explicitly removed.
		keepEmpty := i >= len(groups) || len(originalKeys[i]) == 0
		if section == "openai-compatibility" && i < len(groups) {
			keepEmpty = seen[upstreamV8Reference{Group: i, Key: -1}]
		}
		if len(keys) > 0 || keepEmpty {
			out = append(out, group)
		}
	}
	return out, nil
}
