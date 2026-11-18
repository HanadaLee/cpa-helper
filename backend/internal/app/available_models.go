package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const modelListTimeout = 8 * time.Second

var (
	modelContainerKeys       = []string{"data", "models", "items", "value"}
	modelIDKeys              = []string{"id", "model", "name"}
	modelOwnerKeys           = []string{"owner", "owned_by", "organization"}
	modelReservedMetadataKey = map[string]bool{
		"id": true, "model": true, "name": true,
		"owner": true, "owned_by": true, "organization": true,
		"object": true, "created": true,
	}
)

type AvailableModelSource struct {
	APIKeyHash    string `json:"api_key_hash"`
	APIKeyPreview string `json:"api_key_preview"`
	Description   string `json:"description"`
	UserID        *int   `json:"user_id,omitempty"`
	UserLabel     string `json:"user_label,omitempty"`
}

type AvailableModelPrice struct {
	Provider                                  string   `json:"provider"`
	Model                                     string   `json:"model"`
	InputUSDPerMillion                        float64  `json:"input_usd_per_million"`
	OutputUSDPerMillion                       float64  `json:"output_usd_per_million"`
	CacheReadUSDPerMillion                    float64  `json:"cache_read_usd_per_million"`
	CacheCreationUSDPerMillion                float64  `json:"cache_creation_usd_per_million"`
	RequestUSD                                *float64 `json:"request_usd"`
	FastEnabled                               bool     `json:"fast_enabled"`
	UltraFastEnabled                          bool     `json:"ultrafast_enabled"`
	FastMultiplier                            float64  `json:"fast_multiplier"`
	UltraFastMultiplier                       float64  `json:"ultrafast_multiplier"`
	LongContextEnabled                        bool     `json:"long_context_enabled"`
	LongContextThresholdTokens                int      `json:"long_context_threshold_tokens"`
	LongContextInputUSDPerMillion             float64  `json:"long_context_input_usd_per_million"`
	LongContextOutputUSDPerMillion            float64  `json:"long_context_output_usd_per_million"`
	LongContextCacheReadUSDPerMillion         float64  `json:"long_context_cache_read_usd_per_million"`
	LongContextCacheCreationUSDPerMillion     float64  `json:"long_context_cache_creation_usd_per_million"`
	LongContextFastUnsupported                bool     `json:"long_context_fast_unsupported"`
	LongContextUltraFastUnsupported           bool     `json:"long_context_ultrafast_unsupported"`
	OffPeakEnabled                            bool     `json:"off_peak_enabled"`
	PeakInputUSDPerMillion                    float64  `json:"peak_input_usd_per_million"`
	PeakOutputUSDPerMillion                   float64  `json:"peak_output_usd_per_million"`
	PeakCacheReadUSDPerMillion                float64  `json:"peak_cache_read_usd_per_million"`
	PeakCacheCreationUSDPerMillion            float64  `json:"peak_cache_creation_usd_per_million"`
	PeakRequestUSD                            *float64 `json:"peak_request_usd"`
	LongContextPeakInputUSDPerMillion         float64  `json:"long_context_peak_input_usd_per_million"`
	LongContextPeakOutputUSDPerMillion        float64  `json:"long_context_peak_output_usd_per_million"`
	LongContextPeakCacheReadUSDPerMillion     float64  `json:"long_context_peak_cache_read_usd_per_million"`
	LongContextPeakCacheCreationUSDPerMillion float64  `json:"long_context_peak_cache_creation_usd_per_million"`
	BillingUnit                               string   `json:"billing_unit"`
}

type AvailableModelItem struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Object   *string                `json:"object"`
	Owner    *string                `json:"owner"`
	Created  *int                   `json:"created"`
	Metadata map[string]any         `json:"metadata"`
	Price    *AvailableModelPrice   `json:"price"`
	Sources  []AvailableModelSource `json:"sources"`
}

type AvailableModelKeyError struct {
	APIKeyHash    string `json:"api_key_hash"`
	APIKeyPreview string `json:"api_key_preview"`
	Description   string `json:"description"`
	Message       string `json:"message"`
}

type AvailableModelsResponse struct {
	HasAPIKeys           bool                     `json:"has_api_keys"`
	APIKeyCount          int                      `json:"api_key_count"`
	QueryableAPIKeyCount int                      `json:"queryable_api_key_count"`
	QuotaPaused          bool                     `json:"quota_paused"`
	Models               []AvailableModelItem     `json:"models"`
	Errors               []AvailableModelKeyError `json:"errors"`
}

func (a *App) handleAvailableModels(w http.ResponseWriter, r *http.Request) error {
	if err := requireMethod(r, http.MethodGet); err != nil {
		return err
	}
	user, err := a.readyUser(r.Context(), r)
	if err != nil {
		return err
	}
	response, err := a.availableModelsForUser(r.Context(), user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, response)
	return nil
}

func (a *App) availableModelsForUser(ctx context.Context, userID int) (AvailableModelsResponse, error) {
	bindings, err := a.userAPIKeys(ctx, userID)
	if err != nil {
		return AvailableModelsResponse{}, err
	}
	response := AvailableModelsResponse{
		HasAPIKeys:  len(bindings) > 0,
		APIKeyCount: len(bindings),
		Models:      []AvailableModelItem{},
		Errors:      []AvailableModelKeyError{},
	}
	if len(bindings) == 0 {
		return response, nil
	}
	user, err := a.getUser(ctx, userID)
	if err != nil {
		return AvailableModelsResponse{}, err
	}
	if user.QuotaPausedAt != nil {
		response.QuotaPaused = true
		return response, nil
	}
	queryable := make([]UserAPIKey, 0, len(bindings))
	for _, binding := range bindings {
		if !binding.Disabled && binding.APIKey != nil && strings.TrimSpace(*binding.APIKey) != "" {
			queryable = append(queryable, binding)
		}
	}
	response.QueryableAPIKeyCount = len(queryable)
	if len(queryable) == 0 {
		return response, nil
	}
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return AvailableModelsResponse{}, err
	}
	prices, err := a.priceMap(ctx)
	if err != nil {
		return AvailableModelsResponse{}, err
	}
	modelsByID := map[string]AvailableModelItem{}
	for _, binding := range queryable {
		source := availableModelSource(binding)
		models, err := fetchAvailableModelItems(ctx, cfg, *binding.APIKey)
		if err != nil {
			response.Errors = append(response.Errors, AvailableModelKeyError{
				APIKeyHash:    source.APIKeyHash,
				APIKeyPreview: source.APIKeyPreview,
				Description:   source.Description,
				Message:       err.Error(),
			})
			continue
		}
		for _, raw := range models {
			model := parseAvailableModel(raw, source)
			if model == nil {
				continue
			}
			existing, ok := modelsByID[model.ID]
			if !ok {
				modelsByID[model.ID] = *model
				continue
			}
			mergeAvailableModel(&existing, *model)
			modelsByID[model.ID] = existing
		}
	}
	if len(response.Errors) > 0 && len(modelsByID) == 0 {
		messages := make([]string, 0, len(response.Errors))
		for _, item := range response.Errors {
			messages = append(messages, item.Description+": "+item.Message)
			if len(messages) >= 3 {
				break
			}
		}
		return AvailableModelsResponse{}, validationError("查询可用模型失败：" + strings.Join(messages, "；"))
	}
	for _, model := range modelsByID {
		if price := findMatchingPrice(prices, &model.ID); price != nil {
			model.Price = &AvailableModelPrice{
				Provider:                                  price.Provider,
				Model:                                     price.Model,
				InputUSDPerMillion:                        price.InputUSDPerMillion,
				OutputUSDPerMillion:                       price.OutputUSDPerMillion,
				CacheReadUSDPerMillion:                    price.CacheReadUSDPerMillion,
				CacheCreationUSDPerMillion:                price.CacheCreationUSDPerMillion,
				RequestUSD:                                price.RequestUSD,
				FastMultiplier:                            price.FastMultiplier,
				FastEnabled:                               price.FastEnabled,
				UltraFastEnabled:                          price.UltraFastEnabled,
				UltraFastMultiplier:                       price.UltraFastMultiplier,
				LongContextUltraFastUnsupported:           price.LongContextUltraFastUnsupported,
				LongContextEnabled:                        price.LongContextEnabled,
				LongContextThresholdTokens:                price.LongContextThresholdTokens,
				LongContextInputUSDPerMillion:             price.LongContextInputUSDPerMillion,
				LongContextOutputUSDPerMillion:            price.LongContextOutputUSDPerMillion,
				LongContextCacheReadUSDPerMillion:         price.LongContextCacheReadUSDPerMillion,
				LongContextCacheCreationUSDPerMillion:     price.LongContextCacheCreationUSDPerMillion,
				LongContextFastUnsupported:                price.LongContextFastUnsupported,
				OffPeakEnabled:                            price.OffPeakEnabled,
				PeakInputUSDPerMillion:                    price.PeakInputUSDPerMillion,
				PeakOutputUSDPerMillion:                   price.PeakOutputUSDPerMillion,
				PeakCacheReadUSDPerMillion:                price.PeakCacheReadUSDPerMillion,
				PeakCacheCreationUSDPerMillion:            price.PeakCacheCreationUSDPerMillion,
				PeakRequestUSD:                            price.PeakRequestUSD,
				LongContextPeakInputUSDPerMillion:         price.LongContextPeakInputUSDPerMillion,
				LongContextPeakOutputUSDPerMillion:        price.LongContextPeakOutputUSDPerMillion,
				LongContextPeakCacheReadUSDPerMillion:     price.LongContextPeakCacheReadUSDPerMillion,
				LongContextPeakCacheCreationUSDPerMillion: price.LongContextPeakCacheCreationUSDPerMillion,
				BillingUnit:                               price.BillingUnit,
			}
		}
		response.Models = append(response.Models, model)
	}
	sort.Slice(response.Models, func(i, j int) bool {
		return strings.ToLower(response.Models[i].ID) < strings.ToLower(response.Models[j].ID)
	})
	return response, nil
}

func fetchAvailableModelItems(ctx context.Context, cfg AppConfig, apiKey string) ([]any, error) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	response, payload, err := doJSON(ctx, httpClient(modelListTimeout), http.MethodGet, makeURL(cfg.Collector.CLIProxyURL, "/v1/models", nil), headers, nil)
	if err != nil {
		return nil, fmt.Errorf("模型列表请求失败：%s", err.Error())
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("模型列表请求失败：HTTP %d", response.StatusCode)
	}
	var raw any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("模型列表响应不是有效 JSON")
	}
	return extractAvailableModelItems(raw)
}

func extractAvailableModelItems(payload any) ([]any, error) {
	if items, ok := payload.([]any); ok {
		return items, nil
	}
	object, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("模型列表响应格式不支持")
	}
	for _, key := range modelContainerKeys {
		if value, exists := object[key]; exists {
			if items, ok := value.([]any); ok {
				return items, nil
			}
			return nil, fmt.Errorf("模型列表响应字段 %s 不是列表", key)
		}
	}
	for _, key := range modelIDKeys {
		if _, exists := object[key]; exists {
			return []any{object}, nil
		}
	}
	return nil, fmt.Errorf("模型列表响应缺少模型列表")
}

func parseAvailableModel(raw any, source AvailableModelSource) *AvailableModelItem {
	if text, ok := raw.(string); ok {
		modelID := strings.TrimSpace(text)
		if modelID == "" {
			return nil
		}
		return &AvailableModelItem{
			ID:       modelID,
			Name:     modelID,
			Metadata: map[string]any{},
			Sources:  []AvailableModelSource{source},
		}
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	modelID := firstStringValue(object, modelIDKeys)
	if modelID == nil {
		return nil
	}
	name := modelID
	if value := stringValue(object["name"]); value != nil {
		name = value
	}
	return &AvailableModelItem{
		ID:       *modelID,
		Name:     *name,
		Object:   stringValue(object["object"]),
		Owner:    firstStringValue(object, modelOwnerKeys),
		Created:  intValuePtr(object["created"]),
		Metadata: metadataFromModelObject(object),
		Sources:  []AvailableModelSource{source},
	}
}

func mergeAvailableModel(target *AvailableModelItem, incoming AvailableModelItem) {
	if target.Name == target.ID && incoming.Name != incoming.ID {
		target.Name = incoming.Name
	}
	if target.Object == nil {
		target.Object = incoming.Object
	}
	if target.Owner == nil {
		target.Owner = incoming.Owner
	}
	if target.Created == nil {
		target.Created = incoming.Created
	}
	if target.Metadata == nil {
		target.Metadata = map[string]any{}
	}
	for key, value := range incoming.Metadata {
		if _, exists := target.Metadata[key]; !exists {
			target.Metadata[key] = value
		}
	}
	seen := map[string]bool{}
	for _, source := range target.Sources {
		seen[source.APIKeyHash] = true
	}
	for _, source := range incoming.Sources {
		if !seen[source.APIKeyHash] {
			target.Sources = append(target.Sources, source)
		}
	}
	sort.Slice(target.Sources, func(i, j int) bool {
		left := strings.ToLower(target.Sources[i].Description) + target.Sources[i].APIKeyHash
		right := strings.ToLower(target.Sources[j].Description) + target.Sources[j].APIKeyHash
		return left < right
	})
}

func metadataFromModelObject(object map[string]any) map[string]any {
	metadata := map[string]any{}
	for key, value := range object {
		if modelReservedMetadataKey[key] || !isJSONScalar(value) {
			continue
		}
		metadata[key] = value
	}
	return metadata
}

func availableModelSource(binding UserAPIKey) AvailableModelSource {
	description := strings.TrimSpace(binding.Description)
	if description == "" {
		description = "未命名 Key"
	}
	return AvailableModelSource{
		APIKeyHash:    binding.APIKeyHash,
		APIKeyPreview: maskSecret(binding.APIKey),
		Description:   description,
	}
}

func firstStringValue(object map[string]any, keys []string) *string {
	for _, key := range keys {
		if value := stringValue(object[key]); value != nil {
			return value
		}
	}
	return nil
}

func stringValue(value any) *string {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func intValuePtr(value any) *int {
	switch typed := value.(type) {
	case float64:
		if typed < 0 {
			return nil
		}
		converted := int(typed)
		return &converted
	case int:
		if typed < 0 {
			return nil
		}
		return &typed
	default:
		return nil
	}
}

func isJSONScalar(value any) bool {
	switch value.(type) {
	case nil, string, float64, bool:
		return true
	default:
		return false
	}
}
