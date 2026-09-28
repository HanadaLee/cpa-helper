-- +goose Up
ALTER TABLE app_settings ADD COLUMN oidc_settings TEXT NOT NULL DEFAULT '{}';
ALTER TABLE app_settings ADD COLUMN sso_default_login TEXT NOT NULL DEFAULT '';
UPDATE app_settings SET sso_default_login = 'cas' WHERE cas_enabled = 1 AND cas_default_login = 1;

CREATE TABLE oidc_identities (
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    user_id INTEGER NOT NULL UNIQUE REFERENCES users(id),
    PRIMARY KEY (issuer, subject)
);
CREATE TABLE oidc_flows (
    state_hash TEXT PRIMARY KEY,
    browser_hash TEXT NOT NULL,
    kind TEXT NOT NULL,
    nonce TEXT NOT NULL DEFAULT '',
    verifier TEXT NOT NULL DEFAULT '',
    config_hash TEXT NOT NULL DEFAULT '',
    return_to TEXT NOT NULL DEFAULT '/',
    expires_at INTEGER NOT NULL
);
CREATE INDEX idx_oidc_flows_expiry ON oidc_flows(expires_at);
CREATE TABLE oidc_sessions (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    issuer TEXT NOT NULL,
    client_id TEXT NOT NULL,
    public_url TEXT NOT NULL,
    logout_url TEXT NOT NULL,
    id_token BLOB NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX idx_oidc_sessions_expiry ON oidc_sessions(expires_at);

-- +goose Down
DROP TABLE oidc_sessions;
DROP TABLE oidc_flows;
DROP TABLE oidc_identities;
ALTER TABLE app_settings DROP COLUMN sso_default_login;
ALTER TABLE app_settings DROP COLUMN oidc_settings;
