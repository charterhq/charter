-- +goose Up
-- A version is immutable by its source files: a newer core may compile the same source differently.
ALTER TABLE agent_versions ADD COLUMN source_digest text NOT NULL DEFAULT '';
-- A run with an approval says two things in a conversation: the draft and the outcome.
DROP INDEX conversation_messages_run_role;
-- What the action returned, for the outcome message.
ALTER TABLE drafts ADD COLUMN result jsonb;

-- +goose Down
ALTER TABLE drafts DROP COLUMN result;
CREATE UNIQUE INDEX conversation_messages_run_role ON conversation_messages (run_id, role);
ALTER TABLE agent_versions DROP COLUMN source_digest;
