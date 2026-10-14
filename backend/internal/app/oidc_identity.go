package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cpa-helper/backend/internal/security"
)

func (a *App) resolveOIDCUser(ctx context.Context, cfg AppConfig, subject string, profile casProfile) (AuthUser, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return AuthUser{}, err
	}
	defer tx.Rollback()
	var id int
	err = tx.QueryRowContext(ctx, "SELECT user_id FROM oidc_identities WHERE issuer = ? AND subject = ?", cfg.OIDC.Issuer, subject).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		var casBound bool
		err = tx.QueryRowContext(ctx, "SELECT id, cas_bound FROM users WHERE username = ?", profile.Username).Scan(&id, &casBound)
		if err == nil && !casBound {
			return AuthUser{}, conflictError("同名账号尚未绑定 CAS，请先通过 CAS 登录后再使用 OIDC")
		}
		if errors.Is(err, sql.ErrNoRows) {
			if !cfg.OIDC.AutoCreateUsers {
				return AuthUser{}, forbiddenError("OIDC 用户尚未在 CPA-Helper 中创建")
			}
			lifetime, monthly, weekly, daily := newUserQuotaArgs(cfg.NewUserQuota)
			now := dbTime(time.Now())
			result, err := tx.InsertContext(ctx, `INSERT INTO users
				(username, nickname, is_admin, cas_email, cas_avatar, quota_lifetime_usd, quota_monthly_usd, quota_weekly_usd, quota_daily_usd, created_at, updated_at)
				VALUES (?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?)`,
				profile.Username, normalizeCASProfileValue(firstNonBlank(profile.DisplayName, profile.Username), 240),
				normalizeCASProfileValue(profile.Email, 320), normalizeCASProfileValue(profile.Avatar, 2048),
				lifetime, monthly, weekly, daily, now, now)
			if err != nil {
				return AuthUser{}, err
			}
			userID, err := result.LastInsertId()
			if err != nil {
				return AuthUser{}, err
			}
			id = int(userID)
		} else if err != nil {
			return AuthUser{}, err
		}
		var alreadyBound bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM oidc_identities WHERE user_id = ?)", id).Scan(&alreadyBound); err != nil {
			return AuthUser{}, err
		}
		if alreadyBound {
			return AuthUser{}, conflictError("该用户已绑定其他 OIDC 身份，不能自动合并")
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO oidc_identities (issuer, subject, user_id) VALUES (?, ?, ?)", cfg.OIDC.Issuer, subject, id); err != nil {
			return AuthUser{}, err
		}
	} else if err != nil {
		return AuthUser{}, err
	}
	var disabled sql.NullString
	var username string
	if err := tx.QueryRowContext(ctx, "SELECT username, disabled_at FROM users WHERE id = ?", id).Scan(&username, &disabled); err != nil {
		return AuthUser{}, err
	}
	if disabled.Valid {
		return AuthUser{}, authenticationError("账号已禁用")
	}
	email := normalizeCASProfileValue(profile.Email, 320)
	avatar := normalizeCASProfileValue(profile.Avatar, 2048)
	if _, err := tx.ExecContext(ctx, `UPDATE users SET
		cas_email = CASE WHEN ? = '' THEN cas_email ELSE ? END,
		cas_avatar = CASE WHEN ? = '' THEN cas_avatar ELSE ? END, updated_at = ? WHERE id = ?`,
		email, email, avatar, avatar, dbTime(time.Now()), id); err != nil {
		return AuthUser{}, err
	}
	if err := tx.Commit(); err != nil {
		return AuthUser{}, err
	}
	a.invalidateUsageUsers()
	user, hash, salt, disabledNow, err := a.userCredentialsByUsername(ctx, username)
	if err != nil {
		return AuthUser{}, err
	}
	if disabledNow {
		return AuthUser{}, authenticationError("账号已禁用")
	}
	applyAuthUserConfig(&user, cfg, hash != nil && salt != nil)
	return user, nil
}

func oidcTokenCipher(secret string) (cipher.AEAD, error) {
	key := sha256.Sum256([]byte("cpa-helper-oidc-token:" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func sealOIDCToken(token, secret string) ([]byte, error) {
	aead, err := oidcTokenCipher(secret)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, []byte(token), nil), nil
}

func unsealOIDCToken(data []byte, secret string) (string, error) {
	aead, err := oidcTokenCipher(secret)
	if err != nil {
		return "", err
	}
	if len(data) < aead.NonceSize() {
		return "", errors.New("invalid encrypted OIDC token")
	}
	raw, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], nil)
	return string(raw), err
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) error {
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	clearSessionCookie(w)
	w.Header().Set("Cache-Control", "no-store")
	response := map[string]any{"ok": true, "logout_url": "/login?skipsso=true"}
	cookie, err := r.Cookie(security.SessionCookieName())
	if err == nil {
		identity, ok := readSessionToken(cookie.Value, cfg.SessionSecret)
		if ok {
			switch identity.AuthMethod {
			case "oidc":
				target, err := a.oidcLogoutURL(w, r, cfg, identity)
				if err != nil {
					return err
				}
				response["logout_url"] = target
			case "cas", "":
				if cfg.CAS.Enabled {
					response["cas_logout_url"] = "/cas/logout"
					response["logout_url"] = "/cas/logout"
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, response)
	return nil
}

func (a *App) oidcLogoutURL(w http.ResponseWriter, r *http.Request, cfg AppConfig, identity *security.Identity) (string, error) {
	const localOnly = "/login?skipsso=true&sso_logout=local"
	if identity.UserID == nil {
		return localOnly, nil
	}
	var encrypted []byte
	var clientID, publicURL, endpoint string
	err := a.db.QueryRowContext(r.Context(), `DELETE FROM oidc_sessions WHERE id = ? AND user_id = ?
		RETURNING client_id, public_url, logout_url, id_token`, identity.SessionID, *identity.UserID).
		Scan(&clientID, &publicURL, &endpoint, &encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		return "/login?skipsso=true", nil
	}
	if err != nil {
		return "", err
	}
	if endpoint == "" {
		return localOnly, nil
	}
	token, err := unsealOIDCToken(encrypted, cfg.SessionSecret)
	if err != nil {
		return localOnly, nil
	}
	flow, err := a.newOIDCFlow(r.Context(), "logout", "", "/login?skipsso=true")
	if err != nil {
		return "", err
	}
	setOIDCFlowCookie(w, "logout", flow.Browser, publicURL, false)
	target, err := url.Parse(endpoint)
	if err != nil {
		return localOnly, nil
	}
	query := target.Query()
	query.Set("client_id", clientID)
	query.Set("id_token_hint", token)
	query.Set("post_logout_redirect_uri", strings.TrimRight(publicURL, "/")+"/oidc/logged-out")
	query.Set("state", flow.State)
	target.RawQuery = query.Encode()
	return target.String(), nil
}
