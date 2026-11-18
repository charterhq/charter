-- +goose Up

-- Registry: agents, immutable package versions, team connections, kill switch.
CREATE TABLE agents (
    name        text PRIMARY KEY,
    team        text NOT NULL,
    owner       text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE agent_versions (
    id          uuid PRIMARY KEY,
    agent       text NOT NULL REFERENCES agents(name),
    version     text NOT NULL,
    git_sha     text NOT NULL,
    digest      text NOT NULL,           -- sha256 of the canonical package
    package     jsonb NOT NULL,          -- full package, never mutated
    applied_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (agent, version)
);

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

-- Kill switch (STD-0005, section 6). connection = '' means the whole agent.
CREATE TABLE switches (
    agent       text NOT NULL REFERENCES agents(name),
    connection  text NOT NULL DEFAULT '',
    state       text NOT NULL CHECK (state IN ('on', 'off_owner', 'off_platform')),
    reason      text NOT NULL DEFAULT '',
    changed_by  text NOT NULL DEFAULT '',
    changed_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (agent, connection)
);

-- Runs (STD-0006). One row per launch, including skipped ones.
CREATE TABLE runs (
    id                uuid PRIMARY KEY,
    agent_version_id  uuid NOT NULL REFERENCES agent_versions(id),
    agent             text NOT NULL,
    scenario          text NOT NULL,
    connection        text NOT NULL,
    envelope          jsonb NOT NULL,
    object_key        text NOT NULL DEFAULT '',
    idempotency_key   text NOT NULL DEFAULT '',
    state             text NOT NULL,
    status            text NOT NULL DEFAULT '',
    input             jsonb,
    output            jsonb,
    usage             jsonb NOT NULL DEFAULT '{}',
    error             text NOT NULL DEFAULT '',
    trace_id          text NOT NULL DEFAULT '',
    received_at       timestamptz NOT NULL,
    started_at        timestamptz,
    finished_at       timestamptz,
    updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX runs_agent_idx ON runs (agent, received_at DESC);
CREATE INDEX runs_object_idx ON runs (agent, object_key) WHERE object_key <> '';

-- Accepted deliveries: a repeated delivery maps to the existing run.
CREATE TABLE deliveries (
    source       text NOT NULL,
    delivery_id  text NOT NULL,
    run_id       uuid NOT NULL REFERENCES runs(id),
    received_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source, delivery_id)
);

CREATE TABLE idempotency (
    key         text PRIMARY KEY,
    run_id      uuid NOT NULL REFERENCES runs(id),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE run_steps (
    run_id       uuid NOT NULL REFERENCES runs(id),
    seq          bigint GENERATED ALWAYS AS IDENTITY,
    state        text NOT NULL,
    attempt      int NOT NULL,
    outcome      text NOT NULL,          -- next state or error text
    started_at   timestamptz NOT NULL,
    finished_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, seq)
);

-- Drafts of actions awaiting approval, bound to a parameter fingerprint (STD-0006, I6).
CREATE TABLE drafts (
    id           uuid PRIMARY KEY,
    run_id       uuid NOT NULL REFERENCES runs(id),
    action       jsonb NOT NULL,
    fingerprint  text NOT NULL,
    initiator    text NOT NULL DEFAULT '',
    approved_by  text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- Waits live outside the process (STD-0006, I5).
CREATE TABLE waits (
    id           uuid PRIMARY KEY,
    run_id       uuid NOT NULL REFERENCES runs(id),
    kind         text NOT NULL CHECK (kind IN ('approval', 'answer', 'event')),
    draft_id     uuid REFERENCES drafts(id),
    addressee    text NOT NULL DEFAULT '',
    deadline     timestamptz NOT NULL,
    on_timeout   text NOT NULL CHECK (on_timeout IN ('cancel', 'fallback')),
    state        text NOT NULL CHECK (state IN ('open', 'approved', 'rejected', 'answered', 'expired')),
    resolved_by  text NOT NULL DEFAULT '',
    resolved_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX waits_one_open_per_run ON waits (run_id) WHERE state = 'open';

-- What agents created in external systems (STD-0005, K9).
CREATE TABLE artifacts (
    id          uuid PRIMARY KEY,
    run_id      uuid NOT NULL REFERENCES runs(id),
    agent       text NOT NULL,
    object_key  text NOT NULL DEFAULT '',
    system      text NOT NULL,
    ref         text NOT NULL,
    policy      text NOT NULL DEFAULT 'keep' CHECK (policy IN ('keep', 'close', 'archive')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (run_id, system, ref)
);

-- Business object state for memory: object (PL6).
CREATE TABLE object_state (
    agent       text NOT NULL,
    object_key  text NOT NULL,
    data        jsonb NOT NULL,
    version     bigint NOT NULL DEFAULT 1,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (agent, object_key)
);

-- Run records waiting to be shipped to the journal store.
CREATE TABLE journal_outbox (
    run_id      uuid PRIMARY KEY REFERENCES runs(id),
    record      jsonb NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    shipped_at  timestamptz
);
CREATE INDEX journal_outbox_pending ON journal_outbox (created_at) WHERE shipped_at IS NULL;

CREATE TABLE audit (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor       text NOT NULL,
    action      text NOT NULL,
    target      text NOT NULL,
    details     jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE audit, journal_outbox, object_state, artifacts, waits, drafts, run_steps,
    idempotency, deliveries, runs, switches, connections, agent_versions, agents;
