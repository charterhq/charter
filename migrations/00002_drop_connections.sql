-- +goose Up
-- Connections live in the package version (package -> 'connections'): a team changes them by
-- publishing a new version. The table was never written.
DROP TABLE connections;

-- +goose Down
CREATE TABLE connections (
    name        text PRIMARY KEY,
    team        text NOT NULL,
    owner       text NOT NULL,
    agent       text NOT NULL REFERENCES agents(name),
    spec        jsonb NOT NULL,
    status      text NOT NULL CHECK (status IN ('shadow', 'active', 'paused')),
    git_sha     text NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);
