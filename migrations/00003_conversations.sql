-- +goose Up
-- Conversation history for memory: thread. A conversation is keyed by agent, connection and the
-- ID the channel driver keeps stable (a thread, a chat). seq orders messages of all conversations.
CREATE TABLE conversation_messages (
    seq           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    agent         text NOT NULL,
    connection    text NOT NULL,
    conversation  text NOT NULL,
    role          text NOT NULL CHECK (role IN ('user', 'agent')),
    author        text NOT NULL DEFAULT '',
    text          text NOT NULL,
    run_id        uuid NOT NULL REFERENCES runs(id),
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX conversation_messages_idx ON conversation_messages (agent, connection, conversation, seq);
CREATE UNIQUE INDEX conversation_messages_run_role ON conversation_messages (run_id, role);

-- +goose Down
DROP TABLE conversation_messages;
