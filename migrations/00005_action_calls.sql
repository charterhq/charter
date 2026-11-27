-- +goose Up
-- An action is called at most once (STD-0006, I4): the mark is set before the call, so a repeated
-- step that finds it does not call again and hands the unknown outcome to people.
ALTER TABLE drafts ADD COLUMN called_at timestamptz;
-- A wait closed because the agent was switched off (STD-0005, K6).
ALTER TABLE waits DROP CONSTRAINT waits_state_check;
ALTER TABLE waits ADD CONSTRAINT waits_state_check
    CHECK (state IN ('open', 'approved', 'rejected', 'answered', 'expired', 'cancelled'));

-- +goose Down
UPDATE waits SET state = 'expired' WHERE state = 'cancelled';
ALTER TABLE waits DROP CONSTRAINT waits_state_check;
ALTER TABLE waits ADD CONSTRAINT waits_state_check
    CHECK (state IN ('open', 'approved', 'rejected', 'answered', 'expired'));
ALTER TABLE drafts DROP COLUMN called_at;
