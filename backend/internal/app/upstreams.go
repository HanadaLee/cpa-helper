package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

var upstreamSectionNames = []string{
	"gemini-api-key",
	"codex-api-key",
	"xai-api-key",
	"claude-api-key",
	"vertex-api-key",
	"openai-compatibility",
}

var upstreamSectionSet = func() map[string]struct{} {
	sections := make(map[string]struct{}, len(upstreamSectionNames))
	for _, name := range upstreamSectionNames {
		sections[name] = struct{}{}
	}
	return sections
}()

type upstreamModelDiscoveryRequest struct {
	Section   string            `json:"section"`
	BaseURL   string            `json:"base_url"`
	APIKey    string            `json:"api_key"`
	AuthIndex string            `json:"auth_index"`
	Headers   map[string]string `json:"headers"`
}

type discoveredUpstreamModel struct {
	Name        string `json:"name"`
	Alias       string `json:"alias,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

func (a *App) handleUpstreams(w http.ResponseWriter, r *http.Request) error {
	user, err := a.readyUser(r.Context(), r)
	if err != nil {
		return err
	}
	if !user.IsAdmin {
		return forbiddenError("需要管理员权限")
	}

	if r.URL.Path == "/api/upstreams" || r.URL.Path == "/api/upstreams/" {
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		return a.listUpstreamSections(w, r)
	}
	if r.URL.Path == "/api/upstreams/models" {
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		return a.discoverUpstreamModels(w, r)
	}

	parts := splitPath(r.URL.Path, "/api/upstreams/")
	if len(parts) != 1 {
		return notFoundError("Not Found")
	}
	section := strings.TrimSpace(parts[0])
	if _, ok := upstreamSectionSet[section]; !ok {
		return notFoundError("不支持的上游类型")
	}

	switch r.Method {
	case http.MethodGet:
		return a.getUpstreamSection(w, r, section)
	case http.MethodPut:
		return a.replaceUpstreamSection(w, r, section)
	default:
		return methodNotAllowed()
	}
}

func (a *App) discoverUpstreamModels(w http.ResponseWriter, r *http.Request) error {
	var request upstreamModelDiscoveryRequest
	if err := decodeJSON(r, &request); err != nil {
		return err
	}
	request.Section = strings.TrimSpace(request.Section)
	if _, ok := upstreamSectionSet[request.Section]; !ok {
		return validationError("不支持的上游类型")
	}
	if request.Section == "vertex-api-key" {
		return validationError("Vertex 上游暂不支持自动获取模型")
	}
	if len(request.APIKey) > 16384 || len(request.AuthIndex) > 1024 {
		return validationError("上游认证信息过长")
	}
	if len(request.Headers) > 100 {
		return validationError("自定义请求头最多允许 100 项")
	}

	endpoint, err := upstreamModelsEndpoint(request.Section, request.BaseURL)
	if err != nil {
		return err
	}
	headers, err := upstreamModelHeaders(request)
	if err != nil {
		return err
	}

	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	models := make([]discoveredUpstreamModel, 0)
	seen := make(map[string]struct{})
	for page := 0; page < 20; page++ {
		body, statusCode, requestErr := a.callUpstreamModelEndpoint(r.Context(), cfg, endpoint, request.AuthIndex, headers)
		if requestErr != nil {
			return requestErr
		}
		if (statusCode < 200 || statusCode >= 300) && request.Section == "openai-compatibility" && (len(headers) > 0 || strings.TrimSpace(request.AuthIndex) != "") {
			fallbackBody, fallbackStatus, fallbackErr := a.callUpstreamModelEndpoint(r.Context(), cfg, endpoint, "", nil)
			if fallbackErr == nil && fallbackStatus >= 200 && fallbackStatus < 300 {
				body, statusCode = fallbackBody, fallbackStatus
			}
		}
		if statusCode < 200 || statusCode >= 300 {
			message := fmt.Sprintf("上游模型接口返回 HTTP %d", statusCode)
			if detail := briefAny(body); detail != "" {
				message += "：" + detail
			}
			return appError("upstream_model_fetch_failed", http.StatusBadGateway, message)
		}

		pageModels, nextPageToken, decodeErr := decodeDiscoveredUpstreamModels(body, request.Section)
		if decodeErr != nil {
			return decodeErr
		}
		for _, model := range pageModels {
			key := strings.ToLower(model.Name)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			models = append(models, model)
			if len(models) > 10000 {
				return appError("upstream_model_fetch_failed", http.StatusBadGateway, "上游返回的模型数量超过 10000")
			}
		}
		if request.Section != "gemini-api-key" || nextPageToken == "" {
			break
		}
		parsed, _ := url.Parse(endpoint)
		query := parsed.Query()
		query.Set("pageToken", nextPageToken)
		parsed.RawQuery = query.Encode()
		endpoint = parsed.String()
	}

	sort.Slice(models, func(i, j int) bool {
		return strings.ToLower(models[i].Name) < strings.ToLower(models[j].Name)
	})
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
	return nil
}

func upstreamModelsEndpoint(section, rawBaseURL string) (string, error) {
	baseURL := strings.TrimSpace(rawBaseURL)
	if baseURL == "" {
		switch section {
		case "gemini-api-key":
			baseURL = "https://generativelanguage.googleapis.com"
		case "claude-api-key":
			baseURL = "https://api.anthropic.com"
		default:
			return "", validationError("请先填写服务地址")
		}
	}
	if len(baseURL) > 4096 {
		return "", validationError("服务地址过长")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", validationError("服务地址必须是有效的 HTTP 或 HTTPS URL")
	}
	parsed.Fragment = ""
	path := strings.TrimRight(parsed.Path, "/")
	lowerPath := strings.ToLower(path)

	switch section {
	case "openai-compatibility":
		if !strings.HasSuffix(lowerPath, "/models") {
			path += "/models"
		}
	case "gemini-api-key":
		path = stripUpstreamAPIVersion(path, "v1beta") + "/v1beta/models"
	case "claude-api-key":
		path = stripUpstreamAPIVersion(path, "v1") + "/v1/models"
	case "codex-api-key", "xai-api-key":
		if strings.HasSuffix(lowerPath, "/v1/models") {
			// The full endpoint is already configured.
		} else if strings.HasSuffix(lowerPath, "/v1") {
			path += "/models"
		} else {
			path += "/v1/models"
		}
	default:
		return "", validationError("此上游类型不支持自动获取模型")
	}
	if path == "" || path[0] != '/' {
		path = "/" + path
	}
	parsed.Path = path
	return parsed.String(), nil
}

func stripUpstreamAPIVersion(path, version string) string {
	lowerPath := strings.ToLower(path)
	marker := "/" + strings.ToLower(version)
	if index := strings.LastIndex(lowerPath, marker); index >= 0 {
		rest := lowerPath[index+len(marker):]
		if rest == "" || strings.HasPrefix(rest, "/") {
			return strings.TrimRight(path[:index], "/")
		}
	}
	return strings.TrimRight(path, "/")
}

func upstreamModelHeaders(request upstreamModelDiscoveryRequest) (map[string]string, error) {
	headers := make(map[string]string, len(request.Headers)+2)
	for rawName, rawValue := range request.Headers {
		name := strings.TrimSpace(rawName)
		value := strings.TrimSpace(rawValue)
		if name == "" {
			continue
		}
		if strings.ContainsAny(name, "\r\n") || strings.ContainsAny(value, "\r\n") {
			return nil, validationError("自定义请求头不能包含换行符")
		}
		headers[name] = value
	}
	apiKey := strings.TrimSpace(request.APIKey)
	authIndex := strings.TrimSpace(request.AuthIndex)
	switch request.Section {
	case "gemini-api-key":
		if !hasUpstreamHeader(headers, "x-goog-api-key") {
			if apiKey != "" {
				headers["x-goog-api-key"] = apiKey
			} else if authIndex != "" {
				headers["x-goog-api-key"] = "$TOKEN$"
			}
		}
	case "claude-api-key":
		if !hasUpstreamHeader(headers, "x-api-key") {
			if apiKey != "" {
				headers["x-api-key"] = apiKey
			} else if bearer := upstreamBearerToken(headers); bearer != "" {
				headers["x-api-key"] = bearer
			} else if authIndex != "" {
				headers["x-api-key"] = "$TOKEN$"
			}
		}
		if !hasUpstreamHeader(headers, "anthropic-version") {
			headers["anthropic-version"] = "2023-06-01"
		}
	default:
		if !hasUpstreamHeader(headers, "authorization") {
			if apiKey != "" {
				headers["Authorization"] = "Bearer " + apiKey
			} else if authIndex != "" {
				headers["Authorization"] = "Bearer $TOKEN$"
			}
		}
	}
	return headers, nil
}

func hasUpstreamHeader(headers map[string]string, name string) bool {
	for key := range headers {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}

func upstreamBearerToken(headers map[string]string) string {
	for key, value := range headers {
		if !strings.EqualFold(key, "authorization") {
			continue
		}
		parts := strings.Fields(value)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			return parts[1]
		}
	}
	return ""
}

func (a *App) callUpstreamModelEndpoint(ctx context.Context, cfg AppConfig, endpoint, authIndex string, headers map[string]string) (any, int, error) {
	requestBody := map[string]any{
		"method": "GET",
		"url":    endpoint,
		"data":   "",
	}
	if strings.TrimSpace(authIndex) != "" {
		requestBody["auth_index"] = strings.TrimSpace(authIndex)
	}
	if len(headers) > 0 {
		requestBody["header"] = headers
	}
	_, payload, err := a.keeperRequest(
		ctx,
		cfg,
		http.MethodPost,
		"/v0/management/api-call",
		nil,
		requestBody,
		time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second,
	)
	if err != nil {
		return nil, 0, err
	}
	var result map[string]any
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, 0, appError("upstream_model_fetch_failed", http.StatusBadGateway, "CPA api-call 响应不是有效 JSON")
	}
	statusCode := keeperIntPtr(result["status_code"], result["statusCode"])
	if statusCode == nil {
		return nil, 0, appError("upstream_model_fetch_failed", http.StatusBadGateway, "CPA api-call 响应缺少 status_code")
	}
	body := result["body"]
	if text, ok := body.(string); ok {
		trimmed := strings.TrimSpace(text)
		if trimmed != "" {
			var decoded any
			if json.Unmarshal([]byte(trimmed), &decoded) == nil {
				body = decoded
			}
		}
	}
	return body, *statusCode, nil
}

func decodeDiscoveredUpstreamModels(body any, section string) ([]discoveredUpstreamModel, string, error) {
	var values []any
	var nextPageToken string
	switch typed := body.(type) {
	case nil:
		values = []any{}
	case []any:
		values = typed
	case map[string]any:
		for _, key := range []string{"data", "models"} {
			if candidate, ok := typed[key].([]any); ok {
				values = candidate
				break
			}
		}
		if token, ok := typed["nextPageToken"].(string); ok {
			nextPageToken = strings.TrimSpace(token)
		}
	default:
		return nil, "", appError("upstream_model_fetch_failed", http.StatusBadGateway, "上游模型接口响应格式无效")
	}
	if values == nil {
		return nil, "", appError("upstream_model_fetch_failed", http.StatusBadGateway, "上游模型接口响应缺少模型列表")
	}

	models := make([]discoveredUpstreamModel, 0, len(values))
	for _, value := range values {
		model := discoveredUpstreamModel{}
		switch typed := value.(type) {
		case string:
			model.Name = strings.TrimSpace(typed)
		case map[string]any:
			model.Name = firstUpstreamString(typed, "id", "name", "model")
			model.Alias = firstUpstreamString(typed, "alias")
			model.DisplayName = firstUpstreamString(typed, "display_name", "displayName")
		}
		if section == "gemini-api-key" {
			model.Name = strings.TrimPrefix(strings.TrimPrefix(model.Name, "/"), "models/")
		}
		model.Name = strings.TrimSpace(model.Name)
		if model.Name != "" {
			models = append(models, model)
		}
	}
	return models, nextPageToken, nil
}

func firstUpstreamString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := item[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (a *App) listUpstreamSections(w http.ResponseWriter, r *http.Request) error {
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	native, err := a.loadUpstreamsV8(r, cfg)
	if err != nil {
		return err
	}
	if native != nil {
		sections := make(map[string]any, len(upstreamSectionNames))
		for _, name := range upstreamSectionNames {
			items, err := upstreamV8Items(native.Groups[upstreamV8Family(name)], name)
			if err != nil {
				return err
			}
			sections[name] = items
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{"sections": sections, "api_version": 8, "revision": native.Revision})
		return nil
	}
	_, payload, err := a.keeperRequest(
		r.Context(),
		cfg,
		http.MethodGet,
		"/v0/management/config",
		nil,
		nil,
		time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second,
	)
	if err != nil {
		return err
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(payload, &config); err != nil {
		return validationError("CLIProxyAPI 配置响应不是有效 JSON")
	}

	sections := make(map[string]any, len(upstreamSectionNames))
	for _, name := range upstreamSectionNames {
		sections[name] = []any{}
		raw, ok := config[name]
		if !ok || len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var items []any
		if err := json.Unmarshal(raw, &items); err != nil {
			return validationError("CLIProxyAPI 上游配置格式无效：" + name)
		}
		sections[name] = items
	}

	writeJSON(w, http.StatusOK, map[string]any{"sections": sections})
	return nil
}

func (a *App) getUpstreamSection(w http.ResponseWriter, r *http.Request, section string) error {
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	native, err := a.loadUpstreamsV8(r, cfg)
	if err != nil {
		return err
	}
	if native != nil {
		items, err := upstreamV8Items(native.Groups[upstreamV8Family(section)], section)
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "api_version": 8, "revision": native.Revision})
		return nil
	}
	_, payload, err := a.keeperRequest(
		r.Context(),
		cfg,
		http.MethodGet,
		"/v0/management/"+section,
		nil,
		nil,
		time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second,
	)
	if err != nil {
		return err
	}
	items, err := decodeUpstreamSectionPayload(payload, section)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

func (a *App) replaceUpstreamSection(w http.ResponseWriter, r *http.Request, section string) error {
	var body json.RawMessage
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	var request struct {
		Items      []any  `json:"items"`
		APIVersion int    `json:"api_version"`
		Revision   string `json:"revision"`
	}
	if err := json.Unmarshal(body, &request.Items); err != nil {
		if err := json.Unmarshal(body, &request); err != nil || request.APIVersion != 8 || request.Items == nil {
			return validationError("上游配置必须是 JSON 对象数组")
		}
	}
	items := request.Items
	if len(items) > 10000 {
		return validationError("单个上游类型最多允许 10000 条配置")
	}
	for _, item := range items {
		if _, ok := item.(map[string]any); !ok {
			return validationError("上游配置必须是 JSON 对象数组")
		}
	}

	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	native, err := a.loadUpstreamsV8(r, cfg)
	if err != nil {
		return err
	}
	if native != nil {
		if request.APIVersion != 8 || request.Revision != native.Revision {
			return conflictError("CPA 配置已被修改，请重新加载并核对后再保存")
		}
		groups, err := rebuildUpstreamsV8(native.Groups[upstreamV8Family(section)], items, section)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(groups)
		if err != nil {
			return err
		}
		response, _, err := a.keeperRawRequest(r.Context(), cfg, http.MethodPut,
			"/v8/management/config/api-keys/"+upstreamV8Family(section), nil, payload, "application/json",
			time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second, cpaConfigMaxBytes, http.Header{"If-Match": {request.Revision}})
		if response != nil && (response.StatusCode == http.StatusPreconditionFailed || response.StatusCode == http.StatusPreconditionRequired) {
			return conflictError("CPA 配置已被修改，请重新加载并核对后再保存")
		}
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		return nil
	}
	if request.APIVersion != 0 {
		return conflictError("CPA 配置接口已变更，请重新加载后再保存")
	}
	_, _, err = a.keeperRequest(
		r.Context(),
		cfg,
		http.MethodPut,
		"/v0/management/"+section,
		nil,
		items,
		time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second,
	)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	return nil
}

func decodeUpstreamSectionPayload(payload []byte, section string) ([]any, error) {
	var direct []any
	if err := json.Unmarshal(payload, &direct); err == nil {
		return direct, nil
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(payload, &wrapper); err != nil {
		return nil, validationError("CLIProxyAPI 上游配置响应不是有效 JSON")
	}
	for _, key := range []string{section, "items", "data"} {
		raw, ok := wrapper[key]
		if !ok || len(raw) == 0 || string(raw) == "null" {
			continue
		}
		if err := json.Unmarshal(raw, &direct); err == nil {
			return direct, nil
		}
	}
	return nil, validationError("CLIProxyAPI 上游配置响应格式无效")
}
