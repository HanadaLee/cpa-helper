package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type keeperCredentialLifecycle struct {
	V8Supported          bool           `json:"v8_supported"`
	RequiresSignIn       bool           `json:"requires_sign_in"`
	RenewalState         string         `json:"renewal_state,omitempty"`
	RecoveryStage        string         `json:"recovery_stage,omitempty"`
	AccessTokenExpiresAt *string        `json:"access_token_expires_at,omitempty"`
	NextRefreshAfter     *string        `json:"next_refresh_after,omitempty"`
	Adopted              bool           `json:"adopted"`
	InferenceVerified    bool           `json:"inference_verified"`
	RenewalVerified      bool           `json:"renewal_verified"`
	RefreshError         map[string]any `json:"refresh_error,omitempty"`
	RecoveryHint         string         `json:"recovery_hint,omitempty"`
	SIWCBindSupported    bool           `json:"siwc_bind_supported"`
	UseSIWC              bool           `json:"use_siwc"`
	SIWCStatus           string         `json:"siwc_status,omitempty"`
	SIWCBound            bool           `json:"siwc_bound"`
	SIWCExpiresAt        *string        `json:"siwc_expires_at,omitempty"`
}

func keeperLifecycle(item map[string]any) keeperCredentialLifecycle {
	state := keeperCredentialLifecycle{
		V8Supported:    keeperBool(item["_helper_v8"]),
		RequiresSignIn: keeperBool(item["requires_sign_in"]) || keeperBool(item["reauth_required"]),
		RenewalState:   keeperString(item["renewal_state"]), RecoveryStage: keeperString(item["recovery_stage"]),
		AccessTokenExpiresAt: keeperStringPtr(item["access_token_expires_at"]), NextRefreshAfter: keeperStringPtr(item["next_refresh_after"]),
		Adopted: keeperBool(item["adopted"]), InferenceVerified: keeperBool(item["inference_verified"]), RenewalVerified: keeperBool(item["renewal_verified"]),
		RecoveryHint: keeperString(item["recovery_hint"]), SIWCBindSupported: keeperBool(item["siwc_bind_supported"]),
		UseSIWC: keeperBool(item["use_siwc"]), SIWCStatus: keeperString(item["siwc_status"]), SIWCBound: keeperBool(item["siwc_bound"]), SIWCExpiresAt: keeperStringPtr(item["siwc_expires_at"]),
	}
	if raw, ok := item["refresh_error"].(map[string]any); ok {
		state.RefreshError = map[string]any{"code": keeperString(raw["code"]), "message": keeperString(raw["message"]), "http_status": keeperIntPtr(raw["http_status"])}
	}
	return state
}

// A missing route permits legacy fallback; authentication, validation and server
// errors must never be masked by a second request against the old API.
func (a *App) keeperCompatibleRequest(ctx context.Context, cfg AppConfig, method, current, legacy string, query url.Values, body any) (*http.Response, []byte, error) {
	timeout := time.Duration(cfg.CodexKeeper.CPATimeoutSeconds) * time.Second
	response, payload, err := a.keeperRequest(ctx, cfg, method, current, query, body, timeout)
	if response != nil && response.StatusCode == http.StatusNotFound && legacy != "" && !json.Valid(payload) {
		return a.keeperRequest(ctx, cfg, method, legacy, query, body, timeout)
	}
	return response, payload, err
}

func (a *App) keeperCoreCredentialAction(w http.ResponseWriter, r *http.Request, encodedName, action string) error {
	if err := requireMethod(r, http.MethodPost); err != nil {
		return err
	}
	name, err := keeperAccountNameFromPath(encodedName)
	if err != nil {
		return err
	}
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	body := map[string]any{"name": name}
	path := "/v8/management/credentials/refresh"
	if action == "core-verify" {
		path = "/v8/management/credentials/verify"
	}
	if action == "siwc-bind" {
		path = "/v8/management/oauth/siwc/bind"
	}
	_, payload, err := a.keeperRequest(r.Context(), cfg, http.MethodPost, path, nil, body, 120*time.Second)
	if err != nil {
		return err
	}
	if action == "siwc-bind" {
		var result keeperOAuthStartResponse
		if json.Unmarshal(payload, &result) != nil {
			return validationError("SIWC 授权响应无效")
		}
		if err := validateKeeperOAuthURL(result.URL, "OAuth 授权地址"); err != nil {
			return err
		}
		if err := validateKeeperOAuthState(result.State); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, result)
	} else {
		// Never relay credential material from refresh/verification responses.
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
	return nil
}

func validateKeeperOAuthTarget(request keeperOAuthStartRequest) error {
	for _, value := range []string{request.Name, request.AuthIndex, request.VerifyModel} {
		if len(value) > 1024 || strings.ContainsAny(value, "\x00\r\n") {
			return validationError("OAuth 目标参数无效")
		}
	}
	return nil
}
