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
| `internal/registry` | agents, immutable package versions, kill switch |
| `internal/driver` | driver interfaces |
| `drivers/` | driver implementations; `stub` for tests |
| `migrations/` | PostgreSQL schema |
| `deploy/` | compose and systemd |

## Development

Requires Go and Docker.

```sh
docker compose -f deploy/compose/dev.yml up -d --wait
export CHARTER_TEST_DATABASE_URL='postgres://charter:charter@localhost:55432/charter?sslmode=disable'
make test
```

## License

Apache-2.0, see [LICENSE](LICENSE).
