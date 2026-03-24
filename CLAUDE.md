# Temporal Basics — Learning Project

## Objective
A JSON-driven Temporal workflow engine written in Go. A client sends a full workflow
definition (contact data + step graph) as a JSON payload, and the engine executes it
durably via Temporal. Any sequence of `sendEmail`, `wait`, and `waitForEvent` steps —
including multi-branch logic — can be expressed without changing Go code.

## Project Setup
- **Language:** Go 1.26.1
- **Module:** `temporal-cart-flow`
- **Temporal SDK:** `go.temporal.io/sdk v1.41.1`
- **Infrastructure:** Docker Compose (Temporal Server + Cassandra + Temporal UI)

## Directory Structure

```
temporal_basics/
├── CLAUDE.md
├── README.md
├── config.json              # Workflow input: contact data + step graph
├── docker-compose.yml       # Temporal server, PostgreSQL, Temporal UI
├── go.mod / go.sum
├── activities/
│   ├── send_email.go        # SendEmailActivity(ctx, to, subject, body) → string
│   ├── wait.go              # WaitActivity — exists but NOT called by GenericWorkflow
│   ├── wait_for_event.go    # WaitForEventActivity — exists but NOT called by GenericWorkflow
│   └── shared.go
├── workflow/
│   ├── types.go             # WorkflowInput, Step, ContactInfo, WorkflowDef, etc.
│   └── generic_workflow.go  # GenericWorkflow — JSON step graph interpreter
├── worker/
│   └── main.go              # Registers GenericWorkflow + activities, polls task queue
├── trigger/
│   └── main.go              # Reads config.json → WorkflowInput, starts GenericWorkflow
├── signaler/
│   └── main.go              # Sends a named signal to a running workflow by ID
└── api/
    └── main.go              # HTTP API server — trigger/inspect/signal workflows via REST
```

## How to Run

**1. Start Temporal infrastructure:**
```bash
docker compose up -d
# Temporal UI: http://localhost:8080
# Temporal gRPC: localhost:7233
```

**2. Start the worker (Terminal 1):**
```bash
go run worker/main.go
```

**3. Trigger the workflow (Terminal 2):**
```bash
go run trigger/main.go
# Prints: Started workflow: ID=email-track-workflow-<uuid> RunID=<uuid>
# Blocks until workflow completes, then prints the result summary.
```

**4. Send a signal to a running workflow (Terminal 3 — optional):**
```bash
go run signaler/main.go <workflow-id>
# Sends the "email-open" signal → workflow takes on_event branch
```

**Alternative: Use the HTTP API (Terminal 2 or 3):**
```bash
go run api/main.go
# API server on :8081 (Temporal UI is on :8080)

# Trigger a workflow
curl -X POST http://localhost:8081/workflows \
  -H "Content-Type: application/json" -d @config.json

# Check status
curl http://localhost:8081/workflows/<workflow-id>

# Send a signal
curl -X POST http://localhost:8081/workflows/<workflow-id>/signal \
  -H "Content-Type: application/json" \
  -d '{"signal_name":"email-open","payload":"user clicked"}'
```

**Control branching via `config.json`:**
- Increase `"timeout"` on the `waitForEvent` step → gives time to send a signal → `on_event` branch
- Decrease `"timeout"` → step times out before signal → `on_timeout` branch

## config.json Shape

```json
{
  "data": {
    "contact": { "name": "...", "email": "...", "phone": "..." },
    "trigger":  { "type": "manual", "payload": {} }
  },
  "workflow": {
    "id": "...",
    "task_queue": "...",
    "steps": [ <step>, ... ]
  }
}
```

### Step Types

**sendEmail**
```json
{
  "id": 1, "type": "sendEmail",
  "from": "trigger", "to": 2,
  "properties": {
    "toEmail": "{{contact.email}}",
    "subject": "Hello",
    "body": "Hi {{contact.name}}, ..."
  }
}
```

**wait**
```json
{ "id": 2, "type": "wait", "from": 1, "to": 3, "wait_duration": "60m" }
```

**waitForEvent** (branching)
```json
{
  "id": 3, "type": "waitForEvent", "from": 1,
  "on_event": 4, "on_timeout": 5,
  "event_type": "email-open", "timeout": "3s"
}
```

### Template Placeholders
Properties can reference contact fields:
- `{{contact.name}}`, `{{contact.email}}`, `{{contact.phone}}`

### Routing Rules
- First step: `"from": "trigger"`
- Subsequent steps: `"from": <id>` (informational only — not used by interpreter)
- `"to": null` or absent → terminal step (workflow ends)
- `waitForEvent` follows `on_event` when a signal is received, else `on_timeout`

## Architecture

### GenericWorkflow (`workflow/generic_workflow.go`)
1. Indexes all steps into a `map[int]Step` via `buildStepMap`
2. Finds the start step (`from == "trigger"`) via `findStartStep`
3. Loop: execute current step → follow next step ID → stop when `nextID == nil`
4. Returns a newline-joined summary of all step results

### How Each Step Type is Implemented

| Step type      | Implementation                                       |
|----------------|------------------------------------------------------|
| `sendEmail`    | `workflow.ExecuteActivity(actCtx, SendEmailActivity, ...)` |
| `wait`         | `workflow.Sleep(ctx, d)` — Temporal native durable timer   |
| `waitForEvent` | `workflow.GetSignalChannel(ctx, eventType).ReceiveWithTimeout(ctx, d, &payload)` |

**Important:** `wait` and `waitForEvent` do NOT call activities. They use Temporal's
built-in `workflow.Sleep` and signal primitives directly — this makes them durable and
replay-safe without extra activity timeouts.

### Activities (in `activities/`)
- `SendEmailActivity` — logs email to stdout, sleeps 2s, returns result string
- `WaitActivity` — sleeps for given duration; **currently unused** by `GenericWorkflow`
- `WaitForEventActivity` — simulates event polling; **currently unused** by `GenericWorkflow`

## Coding Conventions

### Types (`workflow/types.go`)
- `Step.From` is `json.RawMessage` — handles both `"trigger"` (string) and integer IDs
- `Step.To`, `Step.OnEvent`, `Step.OnTimeout` are `*int` — `nil` means "no next step"
- Use `IsStartStep()` method on `Step` to check if `From == "trigger"`
- Add new fields to `Step` for new step types; keep `types.go` as the single source of truth for data shapes

### Workflow logic (`workflow/generic_workflow.go`)
- All new step types are added as `case "stepType":` blocks inside the `switch` in `GenericWorkflow`
- Each case must set both `stepResult string` and `nextID *int`
- Use `workflow.WithActivityOptions(ctx, ...)` when calling activities; hardcoded `StartToCloseTimeout: 30*time.Second` is the current default
- Template resolution (`resolveProperties`) happens **in the workflow** before calling activities — keep it that way
- Error format: `fmt.Errorf("step %d <type>: %w", current.ID, err)` — always include step ID and type

### Error handling
- Wrap errors with step context using `%w`
- Parse errors (duration, step ID) should be returned immediately without executing the step
- Unknown step types return an error via the `default:` case

### General Go style
- Keep package structure flat: one package per top-level directory
- No global state; pass `WorkflowInput` as the single workflow argument
- Activity functions take `context.Context` as first arg, return `(T, error)`

## Adding New Step Types

1. Add any new fields to `Step` in `workflow/types.go`
2. Add a `case "stepType":` block in the `switch` in `workflow/generic_workflow.go`
3. If the step needs an activity: create `activities/<step_type>.go`, register it in `worker/main.go`
4. If the step uses a Temporal primitive (timer, signal, query): call it directly in the workflow — no activity needed
5. Set `nextID` from the appropriate routing field (`current.To`, `current.OnEvent`, `current.OnTimeout`)
6. No changes needed to `trigger/main.go`, `signaler/main.go`, or `config.json` parsing

## Known Limitations

- `StartToCloseTimeout` is hardcoded to 30s for all activities. Long-running activities need this increased or made per-step configurable via a `Step` field.
- Duration strings use Go's `time.ParseDuration` format (`s`, `m`, `h`). Days (`d`) are not supported natively — use `"24h"` instead.
- `signaler/main.go` always sends the `"email-open"` signal with a hardcoded payload. Generalise if needed.
- `trigger/main.go` blocks until the workflow completes (`we.Get`). For fire-and-forget, remove that call.

## ToDo
- ~~expose apis to trigger flows with full json~~ ✓ done — see `api/main.go`
- Create a Kafka Queue for interactions


## Instructions on Prompts
- For any new instruction Please ensure that CLAUDE.md and README.md is uptodat