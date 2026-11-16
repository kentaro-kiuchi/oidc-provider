-- +goose Up
CREATE TABLE oidc_clients (
    client_id text PRIMARY KEY,
    secret_hash bytea NOT NULL,
    name text NOT NULL,
    redirect_uris jsonb NOT NULL CHECK (jsonb_typeof(redirect_uris) = 'array'),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Short-lived opaque session, challenge, and authorization-code state is
-- shared by provider replicas and removed after its expiry.
CREATE TABLE runtime_state (
    state_key text PRIMARY KEY,
    value jsonb NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX runtime_state_expiry_idx ON runtime_state (expires_at);

CREATE TABLE provider_settings (
    setting_key text PRIMARY KEY,
    setting_value boolean NOT NULL
);
INSERT INTO provider_settings(setting_key, setting_value)
    VALUES ('initial_oidc_client_seeded', false);

-- +goose Down
DROP TABLE provider_settings;
DROP TABLE runtime_state;
DROP TABLE oidc_clients;
