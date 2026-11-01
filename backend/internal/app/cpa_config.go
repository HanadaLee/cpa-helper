package app

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	cpaConfigMaxBytes        = 8 << 20
	cpaConfigRequestMaxBytes = (cpaConfigMaxBytes * 2) + 1024
)

type cpaConfigPayload struct {
	Content    string `json:"content"`
	APIVersion int    `json:"api_version"`
	Revision   string `json:"revision"`
}

func (a *App) handleCPAConfig(w http.ResponseWriter, r *http.Request) error {
	if _, err := a.adminUser(r.Context(), r); err != nil {
		return err
	}

	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	timeout := time.Duration(cfg.CodexKeeper.CPATimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	switch r.Method {
	case http.MethodGet:
		content, err := a.loadCPAConfigYAML(r, cfg, timeout)
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, content)
		return nil
	case http.MethodPut:
		payload, err := decodeCPAConfigPayload(r)
		if err != nil {
			return err
		}
		if strings.TrimSpace(payload.Content) == "" {
			return validationError("CPA 配置内容不能为空")
		}
		if len(payload.Content) > cpaConfigMaxBytes {
			return validationError("CPA 配置内容超过大小限制")
		}
		// Probe the current API, but never replace the draft's revision with the
		// freshly read ETag: that would silently overwrite a concurrent edit.
		current, err := a.loadCPAConfigYAML(r, cfg, timeout)
		if err != nil {
			return err
		}
		if payload.APIVersion != current.APIVersion {
			return conflictError("CPA 配置接口已变更，请重新加载后再保存")
		}
		if current.APIVersion == 8 && !strongCPAConfigRevision(payload.Revision) {
			return conflictError("CPA 配置版本缺失，请重新加载后再保存")
		}
		path := "/v0/management/config.yaml"
		if current.APIVersion == 8 {
			path = "/v8/management/config.yaml"
		}
		headers := http.Header{}
		if payload.Revision != "" {
			headers.Set("If-Match", payload.Revision)
		}
		response, _, err := a.keeperRawRequest(
			r.Context(),
			cfg,
			http.MethodPut,
			path,
			nil,
			[]byte(payload.Content),
			"application/yaml",
			timeout,
			cpaConfigMaxBytes,
			headers,
		)
		if response != nil && (response.StatusCode == http.StatusPreconditionFailed || response.StatusCode == http.StatusPreconditionRequired) {
			return conflictError("CPA 配置已被修改，请重新加载并核对后再保存")
		}
		if err != nil {
			return err
		}
		content, err := a.loadCPAConfigYAML(r, cfg, timeout)
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, content)
		return nil
	default:
		return methodNotAllowed()
	}
}

func decodeCPAConfigPayload(r *http.Request) (cpaConfigPayload, error) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, cpaConfigRequestMaxBytes+1))
	if err != nil || len(body) > cpaConfigRequestMaxBytes {
		return cpaConfigPayload{}, validationError("CPA 配置请求超过大小限制")
	}
	var payload cpaConfigPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return cpaConfigPayload{}, validationError("请求体不是有效 JSON")
	}
	return payload, nil
}

func (a *App) loadCPAConfigYAML(r *http.Request, cfg AppConfig, timeout time.Duration) (cpaConfigPayload, error) {
	for _, version := range []int{8, 0} {
		path := "/v8/management/config.yaml"
		if version == 0 {
			path = "/v0/management/config.yaml"
		}
		response, payload, err := a.keeperRawRequest(r.Context(), cfg, http.MethodGet, path, nil, nil, "", timeout, cpaConfigMaxBytes)
		if version == 8 && response != nil && response.StatusCode == http.StatusNotFound {
			continue
		}
		if err != nil {
			return cpaConfigPayload{}, err
		}
		revision := response.Header.Get("ETag")
		if version == 8 && !strongCPAConfigRevision(revision) {
			return cpaConfigPayload{}, validationError("CPA V8 未返回有效配置版本，请升级 CPA Core")
		}
		return cpaConfigPayload{Content: string(payload), APIVersion: version, Revision: revision}, nil
	}
	return cpaConfigPayload{}, notFoundError("CPA 配置接口不存在")
}

func strongCPAConfigRevision(value string) bool {
	// Core uses a quoted SHA-256. Do not accept weak tags, lists or wildcards.
	if len(value) != 66 || value[0] != '"' || value[len(value)-1] != '"' {
		return false
	}
	for _, c := range value[1 : len(value)-1] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
