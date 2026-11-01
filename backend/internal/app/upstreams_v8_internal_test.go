package app

import (
	"encoding/json"
	"reflect"
	"testing"
)

func testNativeGroups(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var groups []map[string]any
	if err := json.Unmarshal([]byte(raw), &groups); err != nil {
		t.Fatal(err)
	}
	return groups
}

func TestUpstreamsV8RoundTripPreservesGroupsAndInheritance(t *testing.T) {
	groups := testNativeGroups(t, `[{"name":"production","base-url":"https://example.invalid/v1","priority":3,
		"request-retry":2,"headers":{"X-Group":"keep"},"models":[{"name":"model"}],"disable-cooling":true,
		"keys":[{"api-key":"one","priority":null,"websockets":true,"request-retry":0,"headers":{},"models":[],"disable-cooling":false},
		{"api-key":"two","priority":7,"websockets":false,"request-retry":null,"headers":null,"models":null}]}]`)
	items, err := upstreamV8Items(groups, "codex-api-key")
	if err != nil {
		t.Fatal(err)
	}
	first := items[0].(map[string]any)
	if first["priority"] != float64(3) || first["request-retry"] != float64(0) || first["disable-cooling"] != false || len(first["models"].([]any)) != 0 {
		t.Fatalf("inheritance/false/zero/empty values changed: %+v", first)
	}
	got, err := rebuildUpstreamsV8(groups, items, "codex-api-key")
	if err != nil {
		t.Fatal(err)
	}
	// JSON normalization ignores Go's equivalent slice implementation types.
	encode := func(value any) string { b, _ := json.Marshal(value); return string(b) }
	if encode(got) != encode(groups) {
		t.Fatalf("round trip changed group policies: got %s want %s", encode(got), encode(groups))
	}
	first["priority"] = float64(8)
	second := items[1].(map[string]any)
	delete(second, "headers") // Explicitly clear an inherited group map.
	got, err = rebuildUpstreamsV8(groups, items, "codex-api-key")
	if err != nil {
		t.Fatal(err)
	}
	keys, _ := upstreamV8Keys(got[0])
	if got[0]["name"] != "production" || got[0]["priority"] != float64(3) || keys[0]["priority"] != float64(8) || keys[1]["request-retry"] != nil || len(keys[1]["headers"].(map[string]any)) != 0 {
		t.Fatalf("editing one key damaged its group or sibling: %+v", got)
	}
	got, err = rebuildUpstreamsV8(groups, items[1:], "codex-api-key")
	if err != nil {
		t.Fatal(err)
	}
	keys, _ = upstreamV8Keys(got[0])
	if len(got) != 1 || len(keys) != 1 || keys[0]["api-key"] != "two" {
		t.Fatal("deleting a key removed the wrong credentials")
	}
	if _, err := rebuildUpstreamsV8(groups, []any{items[0], items[0]}, "codex-api-key"); err == nil {
		t.Fatal("duplicate references accepted")
	}
}

func TestUpstreamsV8EndpointEditSplitsOnlyEditedKey(t *testing.T) {
	groups := testNativeGroups(t, `[{"name":"shared","base-url":"https://old.invalid","request-retry":3,
		"keys":[{"api-key":"one","websockets":true},{"api-key":"two","request-retry":null}]}]`)
	items, _ := upstreamV8Items(groups, "codex-api-key")
	items[0].(map[string]any)["base-url"] = "https://new.invalid"
	got, err := rebuildUpstreamsV8(groups, items, "codex-api-key")
	if err != nil || len(got) != 2 {
		t.Fatalf("split: %+v %v", got, err)
	}
	oldKeys, _ := upstreamV8Keys(got[0])
	newKeys, _ := upstreamV8Keys(got[1])
	if got[0]["base-url"] != "https://old.invalid" || oldKeys[0]["api-key"] != "two" ||
		got[1]["base-url"] != "https://new.invalid" || newKeys[0]["api-key"] != "one" || newKeys[0]["websockets"] != true || got[1]["request-retry"] != float64(3) {
		t.Fatalf("sibling endpoint or overrides changed: %+v", got)
	}
}

func TestUpstreamsV8OpenAIAndNewKeys(t *testing.T) {
	groups := testNativeGroups(t, `[{"name":"compatible","base-url":"https://example.invalid","disabled":true,
		"keys":[{"api-key":"one","weight":5,"proxy-url":null},{"api-key":"two","proxy-url":"direct"}]}]`)
	items, _ := upstreamV8Items(groups, "openai-compatibility")
	got, err := rebuildUpstreamsV8(groups, items, "openai-compatibility")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(groups)
	after, _ := json.Marshal(got)
	if string(before) != string(after) {
		t.Fatalf("OpenAI round trip changed config: %s", after)
	}
	got, err = rebuildUpstreamsV8(groups, nil, "openai-compatibility")
	if err != nil || len(got) != 0 {
		t.Fatal("deletion did not remove group")
	}
	newKey := map[string]any{"api-key": "new", "base-url": "https://new.invalid", "priority": float64(4), "websockets": true}
	got, err = rebuildUpstreamsV8(nil, []any{newKey}, "codex-api-key")
	if err != nil || len(got) != 1 {
		t.Fatalf("new group: %+v %v", got, err)
	}
	keys, _ := upstreamV8Keys(got[0])
	if got[0]["base-url"] != newKey["base-url"] || !reflect.DeepEqual(keys[0], map[string]any{"api-key": "new", "websockets": true}) {
		t.Fatalf("provider-only fields written at group level: %+v", got)
	}
}
