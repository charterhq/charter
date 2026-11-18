# Charter

Agent platform by contract. A team describes an agent in a package (role, scenarios with triggers and input and output schemas, tools, autonomy, budget), and Charter runs it: every run goes through the same ten steps, with duplicate filtering, output validation, approvals bound to exact parameters, budgets that stop a run, a kill switch and a run journal.

Charter owns the contract and every run. Existing tools plug in as drivers behind Charter interfaces: an orchestrator for triggers, an LLM and MCP gateway for models and tools, Git as the source of truth, a chat for people, a store for the journal.

Status: early development, not ready for use.

## Layout

| Path | What |
|---|---|
| `cmd/agentd` | core service |
| `cmd/agentctl` | CLI |
| `internal/run` | run state machine (accept, filter, prepare, model, validate, check, approve, execute, respond, record) |
| `internal/pkg` | v1 package format: load, JSON Schema and semantic checks, compile for the engine |
| `internal/registry` | agents, immutable package versions, kill switch |
| `internal/api` | HTTP API `/v1`: launches (alertmanager, raw), approvals, kill switch, runs, agents |
| `internal/driver` | driver interfaces |
| `drivers/` | driver implementations; `stub` for tests |
| `schemas/` | JSON Schemas of the v1 package format |
| `examples/` | example agent packages |
| `migrations/` | PostgreSQL schema |
| `deploy/` | compose and systemd |

## Validate a package

```sh
go run ./cmd/agentctl validate examples/ticket-triage
```

## Run

```sh
agentd apply examples/ticket-triage   # load a package into the registry
agentd serve                          # API on CHARTER_LISTEN, drivers from CHARTER_* variables
agentctl runs ticket-triage           # CHARTER_URL, CHARTER_TOKEN
```

See `deploy/compose/charter.env.example` for driver settings.

## Development

Requires Go and Docker.

```sh
docker compose -f deploy/compose/dev.yml up -d --wait
export CHARTER_TEST_DATABASE_URL='postgres://charter:charter@localhost:55432/charter?sslmode=disable'
make test
```

## License

Apache-2.0, see [LICENSE](LICENSE).
