package app

import (
	_ "embed"
	"errors"
	"html/template"
	"log"
	"net/http"
)

//go:embed oidc_error.html
var oidcErrorHTML string

var oidcErrorTemplate = template.Must(template.New("oidc-error").Parse(oidcErrorHTML))

// OIDC routes are browser navigations, so errors need a recovery page instead of API JSON.
func (a *App) handleOIDCBrowser(w http.ResponseWriter, r *http.Request) error {
	err := a.handleOIDC(w, r)
	if err == nil || r.Method != http.MethodGet {
		return err
	}
	switch r.URL.Path {
	case "/oidc/login", "/oidc/callback", "/oidc/logged-out":
		writeOIDCErrorPage(w, r, err)
		return nil
	default:
		return err
	}
}

func writeOIDCErrorPage(w http.ResponseWriter, r *http.Request, err error) {
	status, message := http.StatusInternalServerError, "服务器暂时无法完成 OIDC 认证，请稍后重试"
	var appErr *AppError
	if errors.As(err, &appErr) {
		status, message = appErr.Status, appErr.Message
	} else {
		// Do not expose database/transport errors or callback query parameters in the page.
		log.Printf("OIDC request failed %s: %v", r.URL.Path, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if err := oidcErrorTemplate.Execute(w, message); err != nil {
		log.Printf("render OIDC error page: %v", err)
	}
}

func oidcAuthorizationError(code string) error {
	message := "OIDC 登录未完成，请重试"
	switch code {
	case "login_required":
		message = "身份服务登录会话已过期，需要重新登录"
	case "interaction_required", "consent_required", "account_selection_required":
		message = "身份服务需要你确认登录或授权"
	case "access_denied":
		message = "OIDC 登录授权已取消或被拒绝"
	case "server_error", "temporarily_unavailable":
		message = "身份服务暂时不可用，请稍后重试"
	}
	return authenticationError(message)
}
