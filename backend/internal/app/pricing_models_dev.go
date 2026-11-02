package app

import (
	"sort"
	"strings"
)

type modelsDevPriceRow struct {
	sourceModel string
	payload     modelPricePayload
}

// The catalog is provider -> models -> cost. Billing matches model IDs only,
// so choose one offering deterministically, preferring the original provider.
func modelsDevPriceRows(catalog map[string]any) ([]modelsDevPriceRow, int, int, int) {
	type candidate struct {
		provider, model string
		entry           map[string]any
		native          bool
	}
	selected := map[string]candidate{}
	total, invalid, duplicates := 0, 0, 0
	for providerID, rawProvider := range catalog {
		provider, ok := rawProvider.(map[string]any)
		if !ok {
			invalid++
			continue
		}
		models, ok := provider["models"].(map[string]any)
		if !ok {
			invalid++
			continue
		}
		for modelID, rawModel := range models {
			total++
			entry, ok := rawModel.(map[string]any)
			if !ok || strings.TrimSpace(providerID) == "" || strings.TrimSpace(modelID) == "" || len(providerID) > 120 || len(modelID) > 180 {
				invalid++
				continue
			}
			item := candidate{provider: providerID, model: modelID, entry: entry, native: modelsDevOriginalProvider(providerID, modelID, entry)}
			key := strings.ToLower(strings.TrimSpace(modelID))
			if previous, exists := selected[key]; exists {
				duplicates++
				if previous.native && !item.native || previous.native == item.native && previous.provider+"/"+previous.model <= item.provider+"/"+item.model {
					continue
				}
			}
			selected[key] = item
		}
	}
	rows := make([]modelsDevPriceRow, 0, len(selected))
	for _, item := range selected {
		payload, ok := modelsDevEntryToPrice(item.provider, item.model, item.entry)
		if !ok {
			invalid++
			continue
		}
		rows = append(rows, modelsDevPriceRow{sourceModel: item.provider + "/" + item.model, payload: payload})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].sourceModel < rows[j].sourceModel })
	return rows, total, invalid, duplicates
}

func modelsDevOriginalProvider(provider, model string, entry map[string]any) bool {
	if canonical, ok := entry["canonical_model_id"].(string); ok && strings.HasPrefix(canonical, provider+"/") {
		return true
	}
	// Older entries do not yet include canonical_model_id.
	model = strings.ToLower(model)
	switch {
	case strings.HasPrefix(model, "gpt-"), strings.HasPrefix(model, "chatgpt-"), strings.HasPrefix(model, "codex-"), model == "o1", model == "o3", model == "o4-mini", strings.HasPrefix(model, "o1-"), strings.HasPrefix(model, "o3-"):
		return provider == "openai"
	case strings.HasPrefix(model, "claude-"):
		return provider == "anthropic"
	case strings.HasPrefix(model, "gemini-"):
		return provider == "google"
	case strings.HasPrefix(model, "grok-"):
		return provider == "xai"
	case strings.HasPrefix(model, "deepseek-"):
		return provider == "deepseek"
	}
	return strings.HasPrefix(model, provider+"/")
}

func modelsDevEntryToPrice(provider, model string, entry map[string]any) (modelPricePayload, bool) {
	payload := modelPricePayload{Provider: strings.TrimSpace(provider), Model: strings.TrimSpace(model)}
	cost, ok := entry["cost"].(map[string]any)
	if !ok {
		return payload, false
	}
	rates, ok := modelsDevTokenRates(cost)
	if !ok {
		return payload, false
	}
	payload.InputUSDPerMillion, payload.OutputUSDPerMillion = rates.InputUSDPerMillion, rates.OutputUSDPerMillion
	payload.CacheReadUSDPerMillion, payload.CacheCreationUSDPerMillion = rates.CacheReadUSDPerMillion, rates.CacheCreationUSDPerMillion
	// Token prices cannot be converted to a fixed per-image/request price.
	if billingUnitForModel(payload.Model) == modelBillingUnitRequest {
		return payload, true
	}
	var tierRates map[string]any
	threshold := 0
	if rawTiers, exists := cost["tiers"]; exists {
		tiers, ok := rawTiers.([]any)
		if !ok || len(tiers) > 1 {
			// This application currently supports one long-context tier. Do not
			// silently flatten several distinct rates into an incorrect bill.
			return payload, false
		}
		if len(tiers) == 1 {
			tierRates, ok = tiers[0].(map[string]any)
			if !ok {
				return payload, false
			}
			condition, ok := tierRates["tier"].(map[string]any)
			if !ok || condition["type"] != "context" {
				return payload, false
			}
			size, ok := numeric(condition["size"])
			if !ok || !finitePositive(size) || size > 2_147_483_647 || size != float64(int(size)) {
				return payload, false
			}
			threshold = int(size)
		}
	} else if legacy, exists := cost["context_over_200k"]; exists {
		tierRates, ok = legacy.(map[string]any)
		if !ok {
			return payload, false
		}
		threshold = 200_000
	}
	if threshold > 0 {
		longRates, ok := modelsDevTokenRates(tierRates)
		if !ok {
			return payload, false
		}
		payload.LongContextEnabled, payload.LongContextThresholdTokens = true, threshold
		payload.LongContextInputUSDPerMillion, payload.LongContextOutputUSDPerMillion = longRates.InputUSDPerMillion, longRates.OutputUSDPerMillion
		payload.LongContextCacheReadUSDPerMillion, payload.LongContextCacheCreationUSDPerMillion = longRates.CacheReadUSDPerMillion, longRates.CacheCreationUSDPerMillion
	}
	return payload, true
}

func modelsDevTokenRates(cost map[string]any) (modelTokenRates, bool) {
	input, inputOK := numeric(cost["input"])
	output, outputOK := numeric(cost["output"])
	if !inputOK || !outputOK || !finiteNonNegative(input) || !finiteNonNegative(output) {
		return modelTokenRates{}, false
	}
	// models.dev already quotes USD per million tokens. An explicit zero is
	// a free rate; an omitted cache rate falls back to the regular input rate.
	rates := modelTokenRates{InputUSDPerMillion: input, OutputUSDPerMillion: output, CacheReadUSDPerMillion: input, CacheCreationUSDPerMillion: input}
	for name, target := range map[string]*float64{"cache_read": &rates.CacheReadUSDPerMillion, "cache_write": &rates.CacheCreationUSDPerMillion} {
		if value, exists := cost[name]; exists {
			number, ok := numeric(value)
			if !ok || !finiteNonNegative(number) {
				return modelTokenRates{}, false
			}
			*target = number
		}
	}
	return rates, true
}
