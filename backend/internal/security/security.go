package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

const (
	sessionCookieName    = "cpa_helper_session"
	sessionMaxAgeSeconds = 60 * 60 * 24 * 7
)

type Identity struct {
	UserID     *int
	Username   *string
	AuthMethod string
	SessionID  string
}

func CreateSalt() (string, error) {
	return randomHex(16)
}

func CreateSecret() (string, error) {
	bytes := make([]byte, 48)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func HashPassword(password, salt string) string {
	digest := pbkdf2.Key([]byte(password), []byte(salt), 260000, 32, sha256.New)
	return base64.URLEncoding.EncodeToString(digest)
}

func VerifyPassword(password, salt, expected string) bool {
	return hmac.Equal([]byte(HashPassword(password, salt)), []byte(expected))
}

func HashAPIKey(apiKey string) string {
	normalized := strings.TrimSpace(apiKey)
	if normalized == "" {
		normalized = "unknown"
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func MaskSecret(value *string) string {
	if value == nil {
		return "unknown"
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return "unknown"
	}
	if normalized == "unknown" {
		return "unknown"
	}
	if len(normalized) <= 4 {
		return "****"
	}
	if len(normalized) <= 8 {
		return normalized[:1] + "..." + normalized[len(normalized)-1:]
	}
	return normalized[:6] + "..." + normalized[len(normalized)-4:]
}

func SetSessionCookie(w http.ResponseWriter, userID int, secret string) error {
	return SetAuthSessionCookie(w, userID, secret, "local", "", false)
}

func SetAuthSessionCookie(w http.ResponseWriter, userID int, secret, method, sessionID string, secure bool) error {
	token, err := createAuthSessionToken(userID, secret, method, sessionID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   sessionMaxAgeSeconds,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
	return nil
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func ReadSessionToken(token, secret string) (*Identity, bool) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0]))
	expected := mac.Sum(nil)
	actual, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		actual, err = base64.URLEncoding.DecodeString(parts[1])
	}
	if err != nil || !hmac.Equal(actual, expected) {
		return nil, false
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		payloadBytes, err = base64.URLEncoding.DecodeString(parts[0])
	}
	if err != nil {
		return nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, false
	}
	expFloat, ok := payload["exp"].(float64)
	if !ok || time.Now().Unix() > int64(expFloat) {
		return nil, false
	}
	sub, _ := payload["sub"].(string)
	typ, _ := payload["typ"].(string)
	if typ == "user_id" {
		id, err := strconv.Atoi(sub)
		if err != nil {
			return nil, false
		}
		method, _ := payload["auth_method"].(string)
		sessionID, _ := payload["sid"].(string)
		return &Identity{UserID: &id, AuthMethod: method, SessionID: sessionID}, true
	}
	if sub != "" {
		return &Identity{Username: &sub}, true
	}
	return nil, false
}

func SessionCookieName() string {
	return sessionCookieName
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func createSessionToken(userID int, secret string) (string, error) {
	return createAuthSessionToken(userID, secret, "local", "")
}

func createAuthSessionToken(userID int, secret, method, sessionID string) (string, error) {
	payload := map[string]any{
		"sub":         strconv.Itoa(userID),
		"typ":         "user_id",
		"exp":         time.Now().Add(sessionMaxAgeSeconds * time.Second).Unix(),
		"auth_method": method,
		"sid":         sessionID,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	payloadPart := base64.RawURLEncoding.EncodeToString(payloadBytes)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payloadPart))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payloadPart + "." + signature, nil
}
