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
| `internal/api` | HTTP API `/v1`: team hooks, calls, launches (alertmanager, raw), approvals, kill switch, runs, agents |
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

## Launches

A scenario names logical sources in `spec.triggers`; a team's connection (`connections/<team>.yml`) binds them to its events. A source the connection does not list does not launch the agent for that team.

```yaml
spec:
  scenarios: [triage]
  triggers:
    alerts: {receiver: alertmanager}            # event source: webhook format, alertmanager or raw
    smoke: {schedule: "CRON_TZ=Europe/Moscow 0 9 * * *", input: {probe: true}}  # schedule source
  channels: {reply: <channel-id>, shadow: <channel-id>}
  status: active                                # shadow: answers go to the shadow channel only, nothing is executed; paused: no launches
```

| Trigger type | How it reaches the core |
|---|---|
| `event` | `POST /v1/hooks/{agent}/{connection}/{source}` with a bearer token, body in the receiver format |
| `schedule` | the core: every minute the River leader launches occurrences due in the last 10 minutes, each once (`CHARTER_SCHEDULE_TZ` for crons without `CRON_TZ`, UTC by default) |
| `call` | `POST /v1/agents/{agent}/scenarios/{scenario}/call?connection=&wait=` or `agentctl call` |
| `message` | a channel driver: `POST /v1/conversations/{agent}/{connection}/{source}` with `conversation`, `message_id`, `author`, `text`, `reply`; or `agentctl say` |

The answer goes to the thread the launch came from, else to `channels.reply`.

## Conversations

A message is a run like any other. The core keeps the conversation (`conversation_messages`), answers messages of one conversation in order and, with `memory: thread`, puts earlier turns into the input:

```json
{"conversation": "...", "message": {"author": "...", "text": "..."}, "history": [{"role": "user|agent", "author": "...", "text": "...", "at": "..."}]}
```

`users` on a message source in the connection lists who may talk to the agent; others get 403 and the driver tells them. The core knows no chat: a driver maps its threads to conversation IDs.

An act scenario in a conversation names the output field with the action arguments, `spec.action_input: draft`. A turn without the field is an answer; a turn with it shows the answer with Approve and Reject buttons bound to the draft fingerprint. A newer message in the conversation replaces a pending draft. After the decision the template block `done` or `rejected` renders the outcome, with `.action.input`, `.action.result` and `.action.by`.

The Mattermost driver listens as each agent bot named in `spec.identities` (`system: mattermost`) with a token in `CHARTER_MM_BOTS`, over a websocket pinged every 30 seconds and reconnected when a pong is late. A post reaches the agent when it names the bot as `@name` (not `@channel` or `@here`) or comes in a direct message; a thread is a conversation, in direct messages the channel is. `channels` on the message source binds a connection to channel IDs. The agent answers as its own bot.

## Development

Requires Go and Docker.

```sh
docker compose -f deploy/compose/dev.yml up -d --wait
export CHARTER_TEST_DATABASE_URL='postgres://charter:charter@localhost:55432/charter?sslmode=disable'
make test
```

## License

Apache-2.0, see [LICENSE](LICENSE).
