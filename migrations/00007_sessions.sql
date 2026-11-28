-- +goose Up
-- People log in through OIDC; a session lives in the database so that any replica serves it.

-- A login in progress: what the callback checks (state) and needs (PKCE verifier, nonce).
CREATE TABLE auth_logins (
    state       text PRIMARY KEY,
    verifier    text NOT NULL,
    nonce       text NOT NULL,
    return_to   text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- id is the SHA-256 of the cookie value: a dump of the table does not log anyone in.
CREATE TABLE sessions (
    id            text PRIMARY KEY,
    person        text NOT NULL,
    subject       text NOT NULL,
    name          text NOT NULL DEFAULT '',
    email         text NOT NULL DEFAULT '',
    groups        text[] NOT NULL DEFAULT '{}',
    refresh_token text NOT NULL DEFAULT '',
    id_token      text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    -- groups come from the provider again after this moment
    refreshed_at  timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL
);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

-- +goose Down
DROP TABLE sessions;
DROP TABLE auth_logins;
