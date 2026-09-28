package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"cpa-helper/backend/internal/security"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const oidcFlowTTL = 5 * time.Minute
const oidcSessionTTL = 7 * 24 * time.Hour

var oidcHTTPClient = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
}

type oidcRuntime struct {
	key       string
	expires   time.Time
	provider  *oidc.Provider
	verifier  *oidc.IDTokenVerifier
	oauth     oauth2.Config
	logoutURL string
}

func (a *App) oidcRuntime(ctx context.Context, cfg OIDCConfig) (*oidcRuntime, error) {
	a.oidcMu.Lock()
	defer a.oidcMu.Unlock()
	key := oidcConfigHash(cfg)
	if a.oidcClient != nil && a.oidcClient.key == key && time.Now().Before(a.oidcClient.expires) {
		return a.oidcClient, nil
	}
	p, err := oidc.NewProvider(oidc.ClientContext(ctx, oidcHTTPClient), cfg.Issuer)
	if err != nil {
		return nil, appError("oidc_unavailable", http.StatusBadGateway, "无法读取 OIDC 服务配置")
	}
	var metadata struct {
		LogoutURL   string `json:"end_session_endpoint"`
		JWKSURL     string `json:"jwks_uri"`
		UserInfoURL string `json:"userinfo_endpoint"`
	}
	if err := p.Claims(&metadata); err != nil {
		return nil, err
	}
	endpoint := p.Endpoint()
	if endpoint.AuthURL == "" || endpoint.TokenURL == "" || metadata.JWKSURL == "" {
		return nil, appError("oidc_invalid_provider", http.StatusBadGateway, "OIDC 服务配置缺少必要端点")
	}
	logoutURL := firstNonBlank(cfg.LogoutURL, metadata.LogoutURL)
	for _, target := range []string{endpoint.AuthURL, endpoint.TokenURL, logoutURL, metadata.JWKSURL, metadata.UserInfoURL} {
		if _, err := normalizeOIDCEndpoint(target, "OIDC 端点"); err != nil {
			return nil, err
		}
	}
	client := &oidcRuntime{
		key: key, expires: time.Now().Add(time.Hour), provider: p,
		verifier: p.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth: oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret,
			RedirectURL: cfg.PublicURL + "/oidc/callback", Endpoint: endpoint,
			Scopes: []string{oidc.ScopeOpenID, "profile", "email"}},
		logoutURL: logoutURL,
	}
	a.oidcClient = client
	return client, nil
}

type oidcFlow struct {
	State      string
	Browser    string
	Nonce      string
	Verifier   string
	ConfigHash string
	ReturnTo   string
}

func (a *App) newOIDCFlow(ctx context.Context, kind, configHash, returnTo string) (oidcFlow, error) {
	var flow oidcFlow
	var err error
	if flow.State, err = createSecret(); err != nil {
		return flow, err
	}
	if flow.Browser, err = createSecret(); err != nil {
		return flow, err
	}
	if flow.Nonce, err = createSecret(); err != nil {
		return flow, err
	}
	flow.Verifier = oauth2.GenerateVerifier()
	flow.ConfigHash = configHash
	flow.ReturnTo = safeCASReturnTo(returnTo)
	// These short-lived rows survive process restarts and make callbacks one-use.
	if _, err = a.db.ExecContext(ctx, "DELETE FROM oidc_flows WHERE expires_at <= ?", time.Now().Unix()); err != nil {
		return flow, err
	}
	_, err = a.db.ExecContext(ctx, `INSERT INTO oidc_flows
		(state_hash, browser_hash, kind, nonce, verifier, config_hash, return_to, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		hashAPIKey(flow.State), hashAPIKey(flow.Browser), kind, flow.Nonce, flow.Verifier,
		flow.ConfigHash, flow.ReturnTo, time.Now().Add(oidcFlowTTL).Unix())
	return flow, err
}

func setOIDCFlowCookie(w http.ResponseWriter, kind, value, publicURL string, clear bool) {
	age := int(oidcFlowTTL.Seconds())
	if clear {
		age = -1
	}
	http.SetCookie(w, &http.Cookie{Name: "cpa_oidc_" + kind, Value: value, Path: "/oidc",
		HttpOnly: true, Secure: strings.HasPrefix(publicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: age})
}

func (a *App) consumeOIDCFlow(w http.ResponseWriter, r *http.Request, kind, publicURL string) (oidcFlow, error) {
	var flow oidcFlow
	cookie, err := r.Cookie("cpa_oidc_" + kind)
	state := r.URL.Query().Get("state")
	if err != nil || cookie.Value == "" || state == "" || len(state) > 200 || len(cookie.Value) > 200 {
		return flow, authenticationError("OIDC 登录状态无效或已过期，请重新登录")
	}
	err = a.db.QueryRowContext(r.Context(), `DELETE FROM oidc_flows
		WHERE state_hash = ? AND browser_hash = ? AND kind = ? AND expires_at > ?
		RETURNING nonce, verifier, config_hash, return_to`,
		hashAPIKey(state), hashAPIKey(cookie.Value), kind, time.Now().Unix()).
		Scan(&flow.Nonce, &flow.Verifier, &flow.ConfigHash, &flow.ReturnTo)
	if errors.Is(err, sql.ErrNoRows) {
		return flow, authenticationError("OIDC 登录状态无效或已过期，请重新登录")
	}
	if err != nil {
		return flow, err
	}
	setOIDCFlowCookie(w, kind, "", publicURL, true)
	return flow, nil
}

func (a *App) handleOIDC(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return methodNotAllowed()
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	if r.URL.Path == "/oidc/logged-out" {
		if _, err := a.consumeOIDCFlow(w, r, "logout", cfg.OIDC.PublicURL); err != nil {
			return err
		}
		http.Redirect(w, r, "/login?skipsso=true", http.StatusFound)
		return nil
	}
	if !cfg.OIDC.Enabled {
		return appError("oidc_disabled", http.StatusServiceUnavailable, "OIDC 登录未启用")
	}
	switch r.URL.Path {
	case "/oidc/login":
		client, err := a.oidcRuntime(r.Context(), cfg.OIDC)
		if err != nil {
			return err
		}
		flow, err := a.newOIDCFlow(r.Context(), "login", client.key, r.URL.Query().Get("returnTo"))
		if err != nil {
			return err
		}
		setOIDCFlowCookie(w, "login", flow.Browser, cfg.OIDC.PublicURL, false)
		http.Redirect(w, r, client.oauth.AuthCodeURL(flow.State, oidc.Nonce(flow.Nonce), oauth2.S256ChallengeOption(flow.Verifier)), http.StatusFound)
		return nil
	case "/oidc/callback":
		return a.handleOIDCCallback(w, r, cfg)
	default:
		return notFoundError("Not Found")
	}
}

func (a *App) handleOIDCCallback(w http.ResponseWriter, r *http.Request, cfg AppConfig) error {
	flow, err := a.consumeOIDCFlow(w, r, "login", cfg.OIDC.PublicURL)
	if err != nil {
		return err
	}
	if flow.ConfigHash != oidcConfigHash(cfg.OIDC) {
		return authenticationError("OIDC 配置已变更，请重新登录")
	}
	if r.URL.Query().Get("error") != "" {
		http.Redirect(w, r, "/login?skipsso=true&sso_error=oidc", http.StatusFound)
		return nil
	}
	code := r.URL.Query().Get("code")
	if code == "" || len(code) > 8192 {
		return authenticationError("OIDC 授权码无效")
	}
	client, err := a.oidcRuntime(r.Context(), cfg.OIDC)
	if err != nil {
		return err
	}
	ctx := oidc.ClientContext(r.Context(), oidcHTTPClient)
	token, err := client.oauth.Exchange(ctx, code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		return authenticationError("OIDC 授权码交换失败，请重新登录")
	}
	raw, _ := token.Extra("id_token").(string)
	verified, err := client.verifier.Verify(ctx, raw)
	if err != nil || verified.Subject == "" || verified.Nonce == "" || verified.Nonce != flow.Nonce || len(verified.Subject) > 512 {
		return authenticationError("OIDC 身份验证失败")
	}
	var claims map[string]any
	if err := verified.Claims(&claims); err != nil {
		return authenticationError("OIDC 用户信息无效")
	}
	username, _ := claims[cfg.OIDC.UsernameClaim].(string)
	if strings.TrimSpace(username) == "" {
		info, err := client.provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
		if err != nil {
			return authenticationError("OIDC 响应缺少配置的用户名字段")
		}
		if info.Subject != verified.Subject {
			return authenticationError("OIDC UserInfo 身份不匹配")
		}
		var extra map[string]any
		if err := info.Claims(&extra); err != nil {
			return authenticationError("OIDC 用户信息无效")
		}
		for key, value := range extra {
			if existing, ok := claims[key]; !ok || existing == "" {
				claims[key] = value
			}
		}
	}
	profile, err := oidcProfileFromClaims(claims, cfg.OIDC.UsernameClaim)
	if err != nil {
		return err
	}
	user, err := a.resolveOIDCUser(r.Context(), cfg, verified.Subject, profile)
	if err != nil {
		return err
	}
	sessionID, err := createSecret()
	if err != nil {
		return err
	}
	sealed, err := sealOIDCToken(raw, cfg.SessionSecret)
	if err != nil {
		return err
	}
	if _, err := a.db.ExecContext(r.Context(), "DELETE FROM oidc_sessions WHERE expires_at <= ?", time.Now().Unix()); err != nil {
		return err
	}
	_, err = a.db.ExecContext(r.Context(), `INSERT INTO oidc_sessions
		(id, user_id, issuer, client_id, public_url, logout_url, id_token, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID, user.ID, cfg.OIDC.Issuer, cfg.OIDC.ClientID, cfg.OIDC.PublicURL, client.logoutURL, sealed, time.Now().Add(oidcSessionTTL).Unix())
	if err != nil {
		return err
	}
	if err := security.SetAuthSessionCookie(w, user.ID, cfg.SessionSecret, "oidc", sessionID, strings.HasPrefix(cfg.OIDC.PublicURL, "https://")); err != nil {
		return err
	}
	http.Redirect(w, r, flow.ReturnTo, http.StatusFound)
	return nil
}

func oidcProfileFromClaims(claims map[string]any, usernameClaim string) (casProfile, error) {
	stringClaim := func(key string) string { value, _ := claims[key].(string); return strings.TrimSpace(value) }
	username := stringClaim(usernameClaim)
	// No display-name/email/sub fallback: a missing identifier must not link a different account.
	if username == "" || len(username) > 120 || strings.ContainsAny(username, "\r\n\t") {
		return casProfile{}, authenticationError("OIDC 响应缺少配置的用户名字段")
	}
	return casProfile{Username: username, DisplayName: stringClaim("name"), Email: stringClaim("email"),
		Avatar: firstNonBlank(stringClaim("picture"), stringClaim("avatar"))}, nil
}
