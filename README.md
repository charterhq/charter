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
| `web/` | web UI (React, Vite); the build is embedded into agentd by `internal/webui` and served at `/ui/` |
| `drivers/` | driver implementations; `stub` for tests |
| `schemas/` | JSON Schemas of the v1 package format |
| `examples/` | example agent packages |
| `migrations/` | PostgreSQL schema |
| `deploy/` | compose and systemd |

## Validate a package

```sh
go run ./cmd/agentctl validate examples/ticket-triage
```

The format is ahead of the core. `agentctl fields` prints what this core does with each field: `executed`, `described` (kept for people and review), `unenforced` (required by the format, not enforced yet: validation warns) or `unsupported` (validation fails when the field is set). A package never passes with a field the core would silently ignore.

A `brief.md` at the package root – the request in plain words, why the agent exists – is not part of a version: it is neither published nor counted in the source digest, so editing it needs no new version.

## Parameters and knowledge

A scenario declares parameters as JSON Schemas with defaults (`spec.params`); a connection sets the team's values (`spec.params`), checked against those schemas, and a parameter without a default must be set by every connection that enables the scenario. Knowledge files live in the package under `knowledge/` and are named in `agent.yml` and in connections (`knowledge: [{path: knowledge/people.yml}]`), up to 256 KiB per agent; the core does not read other repositories. A run gets, after the persona and the instructions, the agent's knowledge, then its connection's, then the parameters; the response template sees them as `.params`. `agentctl eval --connection <name>` runs cases with a team's parameters and knowledge, a case may set its own `params`.

## Evaluate a package

```sh
CHARTER_URL=... CHARTER_TOKEN=... go run ./cmd/agentctl eval examples/ticket-triage
```

Every case of `eval/cases/*.yml` goes through the core the way a live run goes: input schema, the model step with the scenario tools, output schema with one retry. Nothing is published or recorded, no action runs, nobody gets an answer, and the action function is not offered to the model. `expected` fields and typed checks (`path` with `equals`, `matches`, `contains`, `exists`; `action`) are checked by code; a check written as plain text is a criterion for the judge model (`CHARTER_EVAL_JUDGE_CLASS`, the scenario model class by default). Evaluation calls the model and costs money, so it is open to package publishers only.

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

## Objects

An agent with `memory: object` works on business objects that the core keeps: a draft agent and its brief for an intake agent, say. `spec.object` in the card names the JSON Schema of the object state, a template of the first message of a conversation and the stages people see:

```yaml
spec:
  memory: object
  object:
    schema: object/schema.json          # title is the label of a field, description the question an empty field asks
    opener: object/opener.md.tmpl       # .Title, .Missing (fields with .Title, .Question), .Filled, .Total
    stages:
      - {id: request, title: Request, artifacts: [{kind: brief, title: Brief}]}
```

`x-charter-readonly: model` keeps a field away from the model, `x-charter-collection: true` makes an array of objects with an `id` change one item at a time. Every change is checked against the form of the schema (types, no `required` or `minItems`), so a partial state is fine; `required` and `minItems` say what is missing. A new version may add fields to the schema, not remove or retype them: objects outlive versions.

Conversations about an object see the whole state and what it lacks, and only their own history:

```json
{"conversation": "...", "message": {...}, "history": [...], "object": {"id": "...", "title": "...", "stage": "...", "rev": 3, "state": {...}, "missing": ["cases"]}}
```

The model changes the state with `state_ops` in its output: `{"op": "set", "field": "trigger", "value": "..."}`, `{"op": "clear", "field": ...}`, `{"op": "put", "field": "cases", "id": "c3", "value": {...}}`, `{"op": "remove", "field": "cases", "id": "c3"}`. Operations the schema does not allow make the output invalid. They are saved when the output is checked, with the conversation and the run as their source; a field a person changed after the model read the state is left as the person set it.

A message source with `receiver: web` in the connection takes messages through the API below instead of a channel driver. The endpoints are experimental until a second agent uses them:

| Method | Path | |
|---|---|---|
| `GET` | `/v1/me` | `{person, platform}`: who the caller is |
| `POST` | `/v1/agents/{agent}/objects` | `{connection, title, attrs}`; the caller becomes the owner |
| `GET` | `/v1/agents/{agent}/objects?connection=&archived=&cursor=&limit=` | objects where the caller has a role |
| `GET` | `/v1/objects/{id}` | the snapshot: state, fields, missing, sources, stages, members, conversations, `last_seq` |
| `PATCH` | `/v1/objects/{id}` | `{title, attrs}` |
| `DELETE` | `/v1/objects/{id}` | archive |
| `PATCH` | `/v1/objects/{id}/state` | `{fields: {name: {value, rev}}}`; `value: null` clears, 409 `field_conflict` when the same field changed after `rev` |
| `PUT` | `/v1/objects/{id}/members` | `[{person, role}]`, owners only |
| `GET` | `/v1/objects/{id}/events?after=` | server-sent events from `after` or `Last-Event-ID` |
| `POST` | `/v1/objects/{id}/conversations` | `{title}` → the conversation and its first message |
| `PATCH` | `/v1/objects/{id}/conversations/{cid}` | `{title, archived}` |
| `GET` | `/v1/objects/{id}/conversations/{cid}/messages?before=&after=&limit=` | |
| `POST` | `/v1/objects/{id}/conversations/{cid}/messages` | `{message_id, text}` → 202 `{run_id, seq, duplicate}` |

Every change of an object is an event numbered in the transaction of the change: `object`, `members`, `conversation`, `message`, `state`, `run` (`accepted`, `running`, `waiting`, `done` with the status). A stream that needs events no longer kept gets `reset` and reads the snapshot again. Callers named in `CHARTER_PLATFORM` see every object; others get 404 for objects where they have no role. Errors carry a `code` next to the text.

Events are kept 30 days, an object without activity for 30 days goes to the archive, the conversations of an object archived 180 days ago are deleted.

The Mattermost driver serves the logical system `chat` (`CHARTER_MM_SYSTEM`) and listens as each agent bot named in `spec.identities` with that system and a token in `CHARTER_MM_BOTS`, over a websocket pinged every 30 seconds and reconnected when a pong is late. A post reaches the agent when it names the bot as `@name` (not `@channel` or `@here`) or comes in a direct message; a thread is a conversation, in direct messages the channel is. `channels` on the message source binds a connection to channel IDs. The agent answers as its own bot.

## Drivers

The core never names a tool. Packages name logical systems (`chat` in `spec.identities`), the installation maps them to channel drivers, and tool formats stay in drivers: a webhook format of an event source is a format driver (`alertmanager`; `raw` is built in), chat button presses come to the channel driver under `/v1/drivers/<name>/` and reach the core as a neutral decision. The Mattermost texts for a pressed button are `CHARTER_MM_APPROVED_TEXT`, `CHARTER_MM_REJECTED_TEXT`, `CHARTER_MM_CLOSED_TEXT`, `CHARTER_MM_CHANGED_TEXT`, `CHARTER_MM_SAME_APPROVER_TEXT`, `CHARTER_MM_SUSPENDED_TEXT`.

## Development

Requires Go and Docker.

```sh
docker compose -f deploy/compose/dev.yml up -d --wait
export CHARTER_TEST_DATABASE_URL='postgres://charter:charter@localhost:55432/charter?sslmode=disable'
make test
```

The web UI needs Node 22. `make web` builds it into `internal/webui/dist`, the next `go build` embeds it, agentd serves it at `/ui/`. `make web-dev` runs it with hot reload against agentd on `CHARTER_DEV_API` (default `http://localhost:8080`). The builder in the UI answers from a stub until the core has the web conversation API.

## License

Apache-2.0, see [LICENSE](LICENSE).
