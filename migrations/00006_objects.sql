-- +goose Up
-- Business objects for memory: object. object_state was never used by the code and is replaced.
DROP TABLE object_state;

CREATE TABLE objects (
    id              uuid PRIMARY KEY,
    agent           text NOT NULL,
    connection      text NOT NULL,
    key             text NOT NULL,
    title           text NOT NULL DEFAULT '',
    -- attributes only people set by an explicit call (team, agent name): never the model
    attrs           jsonb NOT NULL DEFAULT '{}'::jsonb,
    state           jsonb NOT NULL DEFAULT '{}'::jsonb,
    rev             bigint NOT NULL DEFAULT 0,
    -- the package version whose object schema the object was created with
    agent_version_id uuid NOT NULL REFERENCES agent_versions(id),
    stage           text NOT NULL DEFAULT '',
    last_seq        bigint NOT NULL DEFAULT 0,
    created_by      text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    archived_at     timestamptz,
    UNIQUE (agent, key)
);
CREATE INDEX objects_agent_idx ON objects (agent, connection, updated_at DESC);

-- Roles on an object; changed only by people, never by the model.
CREATE TABLE object_members (
    object_id   uuid NOT NULL REFERENCES objects(id),
    person      text NOT NULL,
    role        text NOT NULL CHECK (role IN ('owner', 'member')),
    added_by    text NOT NULL,
    added_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (object_id, person)
);
CREATE INDEX object_members_person_idx ON object_members (person);

-- Where each state field came from. path is a top-level field or "<collection>/<item id>".
CREATE TABLE object_fields (
    object_id       uuid NOT NULL REFERENCES objects(id),
    path            text NOT NULL,
    rev             bigint NOT NULL,
    source          text NOT NULL CHECK (source IN ('conversation', 'person')),
    conversation_id uuid,
    run_id          uuid REFERENCES runs(id),
    by              text NOT NULL DEFAULT '',
    at              timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (object_id, path)
);

-- Conversations about an object: each sees the whole state, the history is its own.
CREATE TABLE conversations (
    id          uuid PRIMARY KEY,
    object_id   uuid NOT NULL REFERENCES objects(id),
    title       text NOT NULL DEFAULT '',
    opened_by   text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz
);
CREATE INDEX conversations_object_idx ON conversations (object_id, created_at DESC);

-- The event log of an object: seq is issued under the object row lock in the transaction of the
-- change, so events of one object are numbered without gaps in commit order.
CREATE TABLE object_events (
    object_id   uuid NOT NULL REFERENCES objects(id),
    seq         bigint NOT NULL,
    type        text NOT NULL,
    data        jsonb NOT NULL,
    at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (object_id, seq)
);
CREATE INDEX object_events_at_idx ON object_events (at);

ALTER TABLE runs ADD COLUMN object_id uuid REFERENCES objects(id);

-- A message of an object conversation: the client's id, the fields its run changed; the first
-- message of a conversation is written by the core without a run.
ALTER TABLE conversation_messages ADD COLUMN message_id text NOT NULL DEFAULT '';
ALTER TABLE conversation_messages ADD COLUMN fields jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE conversation_messages ALTER COLUMN run_id DROP NOT NULL;

-- +goose Down
DELETE FROM conversation_messages WHERE run_id IS NULL;
ALTER TABLE conversation_messages ALTER COLUMN run_id SET NOT NULL;
ALTER TABLE conversation_messages DROP COLUMN fields;
ALTER TABLE conversation_messages DROP COLUMN message_id;
ALTER TABLE runs DROP COLUMN object_id;
DROP TABLE object_events;
DROP TABLE conversations;
DROP TABLE object_fields;
DROP TABLE object_members;
DROP TABLE objects;
CREATE TABLE object_state (
    agent       text NOT NULL,
    object_key  text NOT NULL,
    data        jsonb NOT NULL,
    version     bigint NOT NULL DEFAULT 1,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (agent, object_key)
);
