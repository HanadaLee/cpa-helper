package app

import (
	"context"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cpa-helper/backend/internal/security"
)

const (
	casRequestTimeout = 10 * time.Second
	casMaxXMLSize     = 1 << 20
)

var casHTTPClient = &http.Client{
	Timeout: casRequestTimeout,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

type casProfile struct {
	Username    string
	Email       string
	DisplayName string
	Avatar      string
}

type casAttribute struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
	Text  string `xml:",chardata"`
}

type casAttributeList struct {
	Attributes []casAttribute `xml:"attribute"`
}

type casAttributes struct {
	Email          string           `xml:"email"`
	DisplayName    string           `xml:"displayName"`
	Avatar         string           `xml:"avatar"`
	Attributes     []casAttribute   `xml:"attribute"`
	UserAttributes casAttributeList `xml:"userAttributes"`
}

type casAuthenticationSuccess struct {
	User        string        `xml:"user"`
	Email       string        `xml:"email"`
	DisplayName string        `xml:"displayName"`
	Avatar      string        `xml:"avatar"`
	Attributes  casAttributes `xml:"attributes"`
}

type casServiceResponse struct {
	Success *casAuthenticationSuccess `xml:"authenticationSuccess"`
	Failure *struct {
		Code string `xml:"code,attr"`
		Text string `xml:",chardata"`
	} `xml:"authenticationFailure"`
}

func (a *App) handleCAS(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return methodNotAllowed()
	}
	switch r.URL.Path {
	case "/cas/login":
		return a.handleCASLogin(w, r)
	case "/cas/callback":
		return a.handleCASCallback(w, r)
	case "/cas/logout":
		return a.handleCASLogout(w, r)
	default:
		return notFoundError("Not Found")
	}
}

func (a *App) handleCASLogin(w http.ResponseWriter, r *http.Request) error {
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	if !cfg.CAS.Enabled {
		return appError("cas_disabled", http.StatusServiceUnavailable, "CAS 登录未启用")
	}
	returnTo := safeCASReturnTo(r.URL.Query().Get("returnTo"))
	loginURL, err := casLoginURL(cfg.CAS, returnTo)
	if err != nil {
		return err
	}
	http.Redirect(w, r, loginURL.String(), http.StatusFound)
	return nil
}

func (a *App) handleCASCallback(w http.ResponseWriter, r *http.Request) error {
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	if !cfg.CAS.Enabled {
		return appError("cas_disabled", http.StatusServiceUnavailable, "CAS 登录未启用")
	}
	ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
	if ticket == "" || len(ticket) > 2048 {
		return authenticationError("CAS Ticket 无效")
	}
	returnTo := safeCASReturnTo(r.URL.Query().Get("returnTo"))
	profile, err := validateCASTicket(r.Context(), cfg.CAS, ticket, returnTo)
	if err != nil {
		var casError *AppError
		if errors.As(err, &casError) && casError.Code == "cas_ticket_invalid" {
			loginPath := "/cas/login?" + url.Values{"returnTo": {returnTo}}.Encode()
			http.Redirect(w, r, loginPath, http.StatusFound)
			return nil
		}
		return err
	}
	user, err := a.resolveCASUser(r.Context(), cfg, profile)
	if err != nil {
		return err
	}
	if err := security.SetAuthSessionCookie(w, user.ID, cfg.SessionSecret, "cas", "", strings.HasPrefix(cfg.CAS.PublicURL, "https://")); err != nil {
		return err
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
	return nil
}

func (a *App) handleCASLogout(w http.ResponseWriter, r *http.Request) error {
	clearSessionCookie(w)
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	if !cfg.CAS.Enabled {
		http.Redirect(w, r, "/login", http.StatusFound)
		return nil
	}
	logoutURL, err := casLogoutURL(cfg.CAS)
	if err != nil {
		return err
	}
	http.Redirect(w, r, logoutURL.String(), http.StatusFound)
	return nil
}

func safeCASReturnTo(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 2048 || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") {
		return "/"
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") {
		return "/"
	}
	return parsed.String()
}

func casLoginURL(cfg CASConfig, returnTo string) (*url.URL, error) {
	loginURL, err := casEndpointURL(cfg.BaseURL, "login")
	if err != nil {
		return nil, err
	}
	serviceURL, err := casServiceURL(cfg, returnTo)
	if err != nil {
		return nil, err
	}
	query := loginURL.Query()
	query.Set("service", serviceURL.String())
	loginURL.RawQuery = query.Encode()
	return loginURL, nil
}

func casLogoutURL(cfg CASConfig) (*url.URL, error) {
	logoutURL, err := casEndpointURL(cfg.BaseURL, "logout")
	if err != nil {
		return nil, err
	}
	publicURL, err := url.Parse(cfg.PublicURL)
	if err != nil {
		return nil, validationError("CAS 公网地址无效")
	}
	query := logoutURL.Query()
	query.Set("service", publicURL.String())
	logoutURL.RawQuery = query.Encode()
	return logoutURL, nil
}

func casServiceURL(cfg CASConfig, returnTo string) (*url.URL, error) {
	callbackURL, err := appendURLPath(cfg.PublicURL, "cas/callback")
	if err != nil {
		return nil, validationError("CAS 公网地址无效")
	}
	query := callbackURL.Query()
	query.Set("returnTo", safeCASReturnTo(returnTo))
	callbackURL.RawQuery = query.Encode()
	return callbackURL, nil
}

func casEndpointURL(baseURL, endpoint string) (*url.URL, error) {
	result, err := appendURLPath(baseURL, endpoint)
	if err != nil {
		return nil, validationError("CAS 服务地址无效")
	}
	return result, nil
}

func appendURLPath(baseURL, path string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("invalid base URL")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(path, "/")
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	return base, nil
}

func validateCASTicket(ctx context.Context, cfg CASConfig, ticket, returnTo string) (casProfile, error) {
	validationBase := cfg.ValidationURL
	if strings.TrimSpace(validationBase) == "" {
		validationBase = cfg.BaseURL
	}
	validationURL, err := casEndpointURL(validationBase, "serviceValidate")
	if err != nil {
		return casProfile{}, err
	}
	serviceURL, err := casServiceURL(cfg, returnTo)
	if err != nil {
		return casProfile{}, err
	}
	query := validationURL.Query()
	query.Set("ticket", ticket)
	query.Set("service", serviceURL.String())
	validationURL.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, validationURL.String(), nil)
	if err != nil {
		return casProfile{}, err
	}
	request.Header.Set("Accept", "application/xml, text/xml")
	if cfg.ValidationHost != "" {
		request.Host = cfg.ValidationHost
	}
	response, err := casHTTPClient.Do(request)
	if err != nil {
		return casProfile{}, appError("cas_unavailable", http.StatusBadGateway, "无法连接 CAS 验证服务")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return casProfile{}, appError("cas_bad_status", http.StatusBadGateway, fmt.Sprintf("CAS 验证服务返回 HTTP %d", response.StatusCode))
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, casMaxXMLSize+1))
	if err != nil {
		return casProfile{}, appError("cas_invalid_response", http.StatusBadGateway, "无法读取 CAS 验证响应")
	}
	if len(payload) > casMaxXMLSize || containsUnsafeXML(payload) {
		return casProfile{}, appError("cas_invalid_response", http.StatusBadGateway, "CAS 返回了不安全的验证响应")
	}
	var result casServiceResponse
	if err := xml.Unmarshal(payload, &result); err != nil {
		return casProfile{}, appError("cas_invalid_response", http.StatusBadGateway, "CAS 返回了无法解析的验证响应")
	}
	if result.Success == nil {
		return casProfile{}, appError("cas_ticket_invalid", http.StatusUnauthorized, "CAS Ticket 无效或已经使用")
	}
	profile := casProfileFromSuccess(*result.Success)
	if profile.Username == "" || len(profile.Username) > 120 {
		return casProfile{}, appError("cas_profile_invalid", http.StatusBadGateway, "CAS 响应缺少有效用户名")
	}
	return profile, nil
}

func containsUnsafeXML(payload []byte) bool {
	text := strings.ToUpper(string(payload))
	return strings.Contains(text, "<!DOCTYPE") || strings.Contains(text, "<!ENTITY")
}

func casProfileFromSuccess(success casAuthenticationSuccess) casProfile {
	profile := casProfile{
		Username:    strings.TrimSpace(success.User),
		Email:       firstNonBlank(success.Email, success.Attributes.Email),
		DisplayName: firstNonBlank(success.DisplayName, success.Attributes.DisplayName),
		Avatar:      firstNonBlank(success.Avatar, success.Attributes.Avatar),
	}
	attributeLists := [][]casAttribute{success.Attributes.Attributes, success.Attributes.UserAttributes.Attributes}
	for _, attributes := range attributeLists {
		for _, attribute := range attributes {
			value := firstNonBlank(attribute.Value, attribute.Text)
			switch strings.ToLower(strings.TrimSpace(attribute.Name)) {
			case "email":
				profile.Email = firstNonBlank(profile.Email, value)
			case "displayname", "display_name":
				profile.DisplayName = firstNonBlank(profile.DisplayName, value)
			case "avatar":
				profile.Avatar = firstNonBlank(profile.Avatar, value)
			}
		}
	}
	return profile
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if normalized := strings.TrimSpace(value); normalized != "" {
			return normalized
		}
	}
	return ""
}

func normalizeCASProfileValue(value string, maxLength int) string {
	value = strings.TrimSpace(value)
	if len(value) > maxLength {
		return ""
	}
	return value
}

func (a *App) syncCASProfile(ctx context.Context, userID int, profile casProfile) error {
	_, err := a.db.ExecContext(ctx, `
		UPDATE users
		SET cas_bound = 1, cas_email = ?, cas_avatar = ?, updated_at = ?
		WHERE id = ?
	`,
		normalizeCASProfileValue(profile.Email, 320),
		normalizeCASProfileValue(profile.Avatar, 2048),
		dbTime(time.Now()),
		userID,
	)
	return err
}

func (a *App) resolveCASUser(ctx context.Context, cfg AppConfig, profile casProfile) (AuthUser, error) {
	user, hash, salt, disabled, err := a.userCredentialsByUsername(ctx, profile.Username)
	if err == nil {
		if disabled {
			return AuthUser{}, authenticationError("账号已禁用")
		}
		if err := a.syncCASProfile(ctx, user.ID, profile); err != nil {
			return AuthUser{}, err
		}
		user.Email = normalizeCASProfileValue(profile.Email, 320)
		user.Avatar = normalizeCASProfileValue(profile.Avatar, 2048)
		user.CASBound = true
		applyAuthUserConfig(&user, cfg, hash != nil && salt != nil)
		return user, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return AuthUser{}, err
	}
	if !cfg.CAS.AutoCreateUsers {
		return AuthUser{}, forbiddenError("CAS 用户尚未在 CPA-Helper 中创建")
	}

	nickname := normalizeCASProfileValue(firstNonBlank(profile.DisplayName, profile.Email, profile.Username), 240)
	email := normalizeCASProfileValue(profile.Email, 320)
	avatar := normalizeCASProfileValue(profile.Avatar, 2048)
	lifetimeQuotaUSD, monthlyQuotaUSD, weeklyQuotaUSD, dailyQuotaUSD := newUserQuotaArgs(cfg.NewUserQuota)
	now := dbTime(time.Now())
	result, insertErr := a.db.InsertContext(ctx, `
		INSERT INTO users (
			username, is_admin, nickname, cas_bound, cas_email, cas_avatar,
			quota_lifetime_usd, quota_monthly_usd, quota_weekly_usd, quota_daily_usd,
			created_at, updated_at
		)
		VALUES (?, 0, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?)
	`, profile.Username, nickname, email, avatar,
		lifetimeQuotaUSD, monthlyQuotaUSD, weeklyQuotaUSD, dailyQuotaUSD, now, now)
	if insertErr != nil {
		user, hash, salt, disabled, err = a.userCredentialsByUsername(ctx, profile.Username)
		if err != nil {
			return AuthUser{}, insertErr
		}
		if disabled {
			return AuthUser{}, authenticationError("账号已禁用")
		}
		if err := a.syncCASProfile(ctx, user.ID, profile); err != nil {
			return AuthUser{}, err
		}
		user.Email = email
		user.Avatar = avatar
		user.CASBound = true
	} else {
		id, _ := result.LastInsertId()
		user = AuthUser{
			ID:        int(id),
			Username:  profile.Username,
			Nickname:  nickname,
			Email:     email,
			Avatar:    avatar,
			CreatedAt: now,
			CASBound:  true,
		}
		a.invalidateUsageUsers()
	}
	applyAuthUserConfig(&user, cfg, hash != nil && salt != nil)
	return user, nil
}
