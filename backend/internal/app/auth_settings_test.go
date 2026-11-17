package app_test

import (
	"net/http"
	"testing"

	backendApp "cpa-helper/backend/internal/app"
)

func TestSettingsPersistCPAMCURL(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)

	var settings struct {
		CPAMCURL string `json:"cpamc_url"`
	}
	requestJSON(t, handler, http.MethodGet, "/api/settings", nil, cookies, &settings)
	if settings.CPAMCURL != "/management.html" {
		t.Fatalf("default cpamc_url = %q, want /management.html", settings.CPAMCURL)
	}

	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cpamc_url": " https://cpamc.example.test/panel?theme=dark ",
	}, cookies, &settings)
	if settings.CPAMCURL != "https://cpamc.example.test/panel?theme=dark" {
		t.Fatalf("updated cpamc_url = %q", settings.CPAMCURL)
	}

	requestJSON(t, handler, http.MethodGet, "/api/settings", nil, cookies, &settings)
	if settings.CPAMCURL != "https://cpamc.example.test/panel?theme=dark" {
		t.Fatalf("persisted cpamc_url = %q", settings.CPAMCURL)
	}

	requestJSONExpectStatus(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cpamc_url": "  ",
	}, cookies, http.StatusUnprocessableEntity)
}

func TestSettingsPersistAndExposeBranding(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	var branding struct {
		BrandNameZH     string `json:"brand_name_zh"`
		BrandNameEN     string `json:"brand_name_en"`
		BrandSubtitleZH string `json:"brand_subtitle_zh"`
		BrandSubtitleEN string `json:"brand_subtitle_en"`
	}
	requestJSON(t, handler, http.MethodGet, "/api/branding", nil, nil, &branding)
	if branding.BrandNameZH != "CPA-Helper" || branding.BrandNameEN != "CPA-Helper" {
		t.Fatalf("default brand names = %q / %q", branding.BrandNameZH, branding.BrandNameEN)
	}
	if branding.BrandSubtitleZH != "边缘网关管理平台" || branding.BrandSubtitleEN != "Edge Gateway Management Platform" {
		t.Fatalf("default brand subtitles = %q / %q", branding.BrandSubtitleZH, branding.BrandSubtitleEN)
	}

	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"brand_name_zh":     " 边缘助手 ",
		"brand_name_en":     " Edge Helper ",
		"brand_subtitle_zh": " 智能网关控制台 ",
		"brand_subtitle_en": " Intelligent Gateway Console ",
	}, cookies, &branding)
	if branding.BrandNameZH != "边缘助手" || branding.BrandNameEN != "Edge Helper" {
		t.Fatalf("updated brand names = %q / %q", branding.BrandNameZH, branding.BrandNameEN)
	}
	requestJSON(t, handler, http.MethodGet, "/api/branding", nil, nil, &branding)
	if branding.BrandSubtitleZH != "智能网关控制台" || branding.BrandSubtitleEN != "Intelligent Gateway Console" {
		t.Fatalf("public brand subtitles = %q / %q", branding.BrandSubtitleZH, branding.BrandSubtitleEN)
	}

	requestJSONExpectStatus(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"brand_name_zh": "  ",
	}, cookies, http.StatusUnprocessableEntity)
}

func TestSettingsRequireAtLeastThirtyOneRetentionDays(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)

	var settings struct {
		UsageDetailRetentionDays int `json:"usage_detail_retention_days"`
	}
	requestJSON(t, handler, http.MethodGet, "/api/settings", nil, cookies, &settings)
	if settings.UsageDetailRetentionDays != 90 {
		t.Fatalf("default usage detail retention = %d, want 90", settings.UsageDetailRetentionDays)
	}
	requestJSONExpectStatus(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"usage_detail_retention_days": 30,
	}, cookies, http.StatusUnprocessableEntity)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"usage_detail_retention_days": 31,
	}, cookies, &settings)
	if settings.UsageDetailRetentionDays != 31 {
		t.Fatalf("updated usage detail retention = %d, want 31", settings.UsageDetailRetentionDays)
	}
}

func TestSettingsConfigureNewUserQuota(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)

	type quotaSettingsResponse struct {
		Unlimited bool    `json:"new_user_quota_unlimited"`
		DailyUSD  float64 `json:"new_user_quota_daily_usd"`
		WeeklyUSD float64 `json:"new_user_quota_weekly_usd"`
	}
	settings := quotaSettingsResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/settings", nil, cookies, &settings)
	if settings.Unlimited || settings.DailyUSD != 0 || settings.WeeklyUSD != 0 {
		t.Fatalf("default new-user quota settings = %+v, want two zero quotas", settings)
	}
	defaultMember := quotaAPIUserResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/users", map[string]any{
		"username": "default-blocked-member", "password": "member-password", "nickname": "Default member",
	}, cookies, &defaultMember)
	if defaultMember.Quota.Unlimited || defaultMember.Quota.CanCreateKeys || defaultMember.Quota.WeeklyQuotaUSD == nil || *defaultMember.Quota.WeeklyQuotaUSD != 0 {
		t.Fatalf("new registration must retain an enabled zero weekly quota: %#v", defaultMember.Quota)
	}

	requestJSONExpectStatus(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"new_user_quota_daily_usd": -0.01,
	}, cookies, http.StatusUnprocessableEntity)

	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"new_user_quota_daily_usd":  1.25,
		"new_user_quota_weekly_usd": 5.5,
	}, cookies, &settings)
	if settings.Unlimited || settings.DailyUSD != 1.25 || settings.WeeklyUSD != 5.5 {
		t.Fatalf("updated new-user quota settings = %+v", settings)
	}

	member := quotaAPIUserResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/users", map[string]any{
		"username": "configured-member",
		"password": "member-password",
		"nickname": "Configured member",
		"is_admin": false,
	}, cookies, &member)
	if member.Quota.Unlimited || member.Quota.WeeklyQuotaUSD == nil || *member.Quota.WeeklyQuotaUSD != 5.5 ||
		member.Quota.DailyQuotaUSD == nil || *member.Quota.DailyQuotaUSD != 1.25 {
		t.Fatalf("configured member quota = %#v", member.Quota)
	}

	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"new_user_quota_unlimited": true,
	}, cookies, &settings)
	unlimitedMember := quotaAPIUserResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/users", map[string]any{
		"username": "unlimited-member",
		"password": "member-password",
		"nickname": "Unlimited member",
		"is_admin": false,
	}, cookies, &unlimitedMember)
	if !unlimitedMember.Quota.Unlimited || !unlimitedMember.Quota.CanCreateKeys {
		t.Fatalf("unlimited member quota = %#v", unlimitedMember.Quota)
	}
}
