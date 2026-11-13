package app

import (
	"encoding/json"
	"math"
	"strings"
)

// Keep Core's accounting evidence separately from the historical token columns.
// Existing charges are immutable; only new ingestion uses this evidence to price.
type cpaTokenBreakdown struct {
	SchemaVersion      int    `json:"schema_version"`
	Quality            string `json:"quality"`
	TotalTokens        int64  `json:"total_tokens"`
	UnclassifiedTokens int64  `json:"unclassified_tokens"`
	Input              struct {
		TotalTokens      int64 `json:"total_tokens"`
		UncachedTokens   int64 `json:"uncached_tokens"`
		CacheReadTokens  int64 `json:"cache_read_tokens"`
		CacheWriteTokens int64 `json:"cache_write_tokens"`
	} `json:"input"`
	Output struct {
		TotalTokens        int64 `json:"total_tokens"`
		NonReasoningTokens int64 `json:"non_reasoning_tokens"`
		ReasoningTokens    int64 `json:"reasoning_tokens"`
	} `json:"output"`
}

func cpaTokenSum(values ...int64) (int64, bool) {
	var sum int64
	for _, value := range values {
		if value < 0 || value > math.MaxInt64-sum {
			return 0, false
		}
		sum += value
	}
	return sum, true
}

func (b cpaTokenBreakdown) complete() bool {
	if b.SchemaVersion != 2 || b.Quality != "complete" || b.UnclassifiedTokens != 0 {
		return false
	}
	input, inputOK := cpaTokenSum(b.Input.UncachedTokens, b.Input.CacheReadTokens, b.Input.CacheWriteTokens)
	output, outputOK := cpaTokenSum(b.Output.NonReasoningTokens, b.Output.ReasoningTokens)
	total, totalOK := cpaTokenSum(input, output)
	return inputOK && outputOK && totalOK && input == b.Input.TotalTokens && output == b.Output.TotalTokens && total == b.TotalTokens
}

var cpaUsageMetadataKeys = []string{
	"accounting_version", "token_breakdown", "executor_type", "upstream_model", "response_model",
	"outbound_service_tier", "response_service_tier", "effective_service_tier", "response_cyber_program",
	"execution_id", "trace_id", "session_id", "parent_session_id", "node_kind", "is_fork", "is_compaction",
	"timing_version", "ttfb_ms", "ttfa_ms", "output_timing", "stream", "fail", "speed", "inference_geo",
	"cache_details", "web_search_requests", "image_count", "requested_image_details", "image_details",
	"media_counts", "tool_usage_details", "iterations", "cost_details",
}

func cpaUsageMetadataJSON(value any) string {
	root, ok := value.(map[string]any)
	if !ok {
		return "{}"
	}
	metadata := make(map[string]any)
	for _, key := range cpaUsageMetadataKeys {
		if value, exists := root[key]; exists {
			metadata[key] = value
		}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return "{}"
	}
	// Do not duplicate arbitrarily large per-iteration details in the hot table.
	// The complete original event remains in raw_json.
	if len(encoded) > 128<<10 {
		for _, key := range []string{"iterations", "tool_usage_details", "output_timing", "cost_details", "image_details", "requested_image_details"} {
			delete(metadata, key)
		}
		encoded, _ = json.Marshal(metadata)
	}
	return string(encoded)
}

func cpaMetadata(record UsageRecord) map[string]json.RawMessage {
	var result map[string]json.RawMessage
	if record.CPAMetadataJSON != "" && record.CPAMetadataJSON != "{}" {
		_ = json.Unmarshal([]byte(record.CPAMetadataJSON), &result)
	} else if record.RawJSON != "" {
		// Read-only detail fallback for retained historical events; no repricing.
		var root any
		if json.Unmarshal([]byte(record.RawJSON), &root) == nil {
			_ = json.Unmarshal([]byte(cpaUsageMetadataJSON(root)), &result)
		}
	}
	return result
}

func cpaAccounting(record UsageRecord) (*cpaTokenBreakdown, bool) {
	metadata := cpaMetadata(record)
	raw, present := metadata["token_breakdown"]
	var version int
	if value, exists := metadata["accounting_version"]; exists {
		if json.Unmarshal(value, &version) != nil || version != 2 {
			return nil, true
		}
	}
	if !present {
		return nil, version != 0
	}
	var breakdown cpaTokenBreakdown
	if json.Unmarshal(raw, &breakdown) != nil || !breakdown.complete() {
		return nil, true
	}
	return &breakdown, true
}

func cpaServiceTier(record UsageRecord) (tier, evidence string) {
	metadata := cpaMetadata(record)
	for _, item := range []struct{ key, evidence string }{{"effective_service_tier", "effective"}, {"response_service_tier", "response"}, {"outbound_service_tier", "outbound"}} {
		var value string
		if raw, ok := metadata[item.key]; ok && json.Unmarshal(raw, &value) == nil && strings.TrimSpace(value) != "" {
			return strings.ToLower(strings.TrimSpace(value)), item.evidence
		}
	}
	if record.RequestServiceTier != nil {
		return strings.ToLower(strings.TrimSpace(*record.RequestServiceTier)), "request"
	}
	return "", "assumed"
}

func cpaUsageDetails(record UsageRecord, redaction usageRedactionOptions) map[string]any {
	metadata := cpaMetadata(record)
	if len(metadata) == 0 {
		return nil
	}
	encoded, _ := json.Marshal(metadata)
	redacted := redactedRawJSON(string(encoded), usageRecordAuth(record), redaction)
	result, _ := redacted.(map[string]any)
	return result
}
