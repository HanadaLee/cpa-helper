package app

import (
	"encoding/json"
	"net/url"
	"strings"
)

type OIDCConfig struct {
	Enabled         bool   `json:"enabled"`
	Issuer          string `json:"issuer"`
	ClientID        string `json:"client_id"`
	ClientSecret    string `json:"client_secret"`
	PublicURL       string `json:"public_url"`
	LogoutURL       string `json:"logout_url"`
	UsernameClaim   string `json:"username_claim"`
	AutoCreateUsers bool   `json:"auto_create_users"`
}

func defaultSSOLogin(cfg AppConfig) string {
	if cfg.SSODefaultLogin == "oidc" && cfg.OIDC.Enabled {
		return "oidc"
	}
	if cfg.SSODefaultLogin == "cas" && cfg.CAS.Enabled {
		return "cas"
	}
	// Retain the old CAS settings API for existing clients.
	if cfg.SSODefaultLogin == "" && cfg.CAS.Enabled && cfg.CAS.DefaultLogin {
		return "cas"
	}
	return ""
}

func normalizeOIDCURL(value, label string) (string, error) {
	return normalizeOIDCAddress(value, label, false)
}

func normalizeOIDCEndpoint(value, label string) (string, error) {
	return normalizeOIDCAddress(value, label, true)
}

func normalizeOIDCAddress(value, label string, allowQuery bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || (!allowQuery && (u.RawQuery != "" || u.ForceQuery)) || u.Fragment != "" ||
		(u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))) {
		if allowQuery {
			return "", validationError(label + "必须为 HTTPS 地址（本机调试可使用 HTTP），且不能包含用户凭据或锚点")
		}
		return "", validationError(label + "必须为 HTTPS 地址（本机调试可使用 HTTP），且不能包含查询参数或锚点")
	}
	return value, nil
}

func normalizeOIDCConfig(cfg OIDCConfig) (OIDCConfig, error) {
	var err error
	if cfg.Issuer, err = normalizeOIDCURL(cfg.Issuer, "OIDC Issuer"); err != nil {
		return cfg, err
	}
	if cfg.PublicURL, err = normalizeOIDCURL(cfg.PublicURL, "OIDC 公网地址"); err != nil {
		return cfg, err
	}
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	if cfg.LogoutURL, err = normalizeOIDCEndpoint(cfg.LogoutURL, "OIDC 退出地址"); err != nil {
		return cfg, err
	}
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.ClientSecret = strings.TrimSpace(cfg.ClientSecret)
	cfg.UsernameClaim = strings.TrimSpace(cfg.UsernameClaim)
	if cfg.UsernameClaim == "" {
		cfg.UsernameClaim = "preferred_username"
	}
	if len(cfg.UsernameClaim) > 120 || strings.ContainsAny(cfg.UsernameClaim, " \r\n\t") {
		return cfg, validationError("OIDC 用户名字段无效")
	}
	if cfg.Enabled && (cfg.Issuer == "" || cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.PublicURL == "") {
		return cfg, validationError("启用 OIDC 时必须填写 Issuer、Client ID、Client Secret 和公网地址")
	}
	return cfg, nil
}

func oidcConfigHash(cfg OIDCConfig) string {
	raw, _ := json.Marshal(cfg)
	return hashAPIKey(string(raw))
}
