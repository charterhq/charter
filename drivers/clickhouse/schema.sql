-- Run journal of the Charter core: one row per run, the full record in STD-0005 field names.
CREATE TABLE IF NOT EXISTS charter_runs
(
    run_id       UUID,
    agent        LowCardinality(String),
    version      LowCardinality(String),
    scenario     LowCardinality(String),
    connection   LowCardinality(String),
    status       LowCardinality(String),
    received_at  DateTime64(3, 'UTC'),
    finished_at  DateTime64(3, 'UTC'),
    input_tokens   UInt32,
    output_tokens  UInt32,
    cost         Float64,
    record       String
)
ENGINE = ReplacingMergeTree(finished_at)
PARTITION BY toYYYYMM(received_at)
ORDER BY (agent, received_at, run_id)
TTL toDateTime(received_at) + INTERVAL 1 YEAR;
