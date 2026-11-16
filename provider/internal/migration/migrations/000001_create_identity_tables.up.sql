-- +goose Up
CREATE TABLE users (
    id             text PRIMARY KEY,
    email          text COLLATE "C",
    email_verified boolean NOT NULL DEFAULT true,
    role           text NOT NULL DEFAULT 'user'
                   CHECK (role IN ('user', 'admin')),
    state          text NOT NULL DEFAULT 'active'
                   CHECK (state IN ('active', 'suspended', 'deleted')),
    created_at     timestamptz NOT NULL DEFAULT now(),
    deleted_at     timestamptz,
    CHECK ((state = 'deleted') = (deleted_at IS NOT NULL)),
    CHECK (
        (state = 'deleted' AND email IS NULL AND NOT email_verified)
        OR
        (state IN ('active', 'suspended') AND email IS NOT NULL AND email_verified)
    )
);

CREATE UNIQUE INDEX users_live_email_unique
    ON users (email COLLATE "C")
    WHERE state IN ('active', 'suspended');

CREATE TABLE passkeys (
    id              text PRIMARY KEY,
    user_id         text NOT NULL REFERENCES users (id),
    credential_id   bytea NOT NULL UNIQUE,
    public_key      bytea NOT NULL,
    sign_count      bigint NOT NULL DEFAULT 0 CHECK (sign_count >= 0),
    aaguid          bytea NOT NULL,
    user_present    boolean NOT NULL DEFAULT true,
    user_verified   boolean NOT NULL DEFAULT true,
    backup_eligible boolean NOT NULL DEFAULT false,
    backup_state    boolean NOT NULL DEFAULT false,
    name            text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    last_used_at    timestamptz
);

CREATE INDEX passkeys_user_id_idx ON passkeys (user_id);

CREATE TABLE email_verification_challenges (
    id          text PRIMARY KEY,
    email       text COLLATE "C" NOT NULL,
    token_hash  bytea NOT NULL UNIQUE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    CHECK (expires_at > created_at)
);

CREATE INDEX email_verification_challenges_expiry_idx
    ON email_verification_challenges (expires_at);

-- +goose Down
DROP TABLE email_verification_challenges;
DROP TABLE passkeys;
DROP TABLE users;
