# temporal-cart-flow

A learning project demonstrating [Temporal](https://temporal.io) workflows in Go.
Models an email open-tracking flow driven by a JSON step graph: wait, send an email, wait for the recipient to open it (via a Temporal signal), then branch based on the outcome.

## What it does

`GenericWorkflow` interprets a step graph defined in `config.json` and executes each step:

1. **wait** — durable sleep for a configured duration (`workflow.Sleep`)
2. **sendEmail** — sends an email (2s simulated delay)
3. **waitForEvent** — blocks on a Temporal signal with a configurable timeout
4. **Branch:**
   - Signal received → `sendEmail` with "Thanks for opening!" body
   - Timed out → `sendEmail` with "We missed you" body

Each workflow run gets a unique ID (`<base-id>-<uuid>`), so multiple runs can be in-flight simultaneously.

## Project structure

```
temporal-cart-flow/
├── config.json                 # Step graph + contact data
├── workflow/
│   ├── generic_workflow.go     # GenericWorkflow — interprets the step graph
│   └── types.go                # WorkflowInput, Step, ContactInfo etc.
├── activities/
│   ├── send_email.go           # SendEmailActivity — 2s fake delay
│   └── wait.go                 # WaitActivity (registered but unused by default)
├── worker/
│   └── main.go                 # Starts the Temporal worker
├── trigger/
│   └── main.go                 # Starts a new workflow run
├── signaler/
│   └── main.go                 # Sends an "email-open" signal to a running workflow
└── docker-compose.yml          # Temporal server + UI
```

## Configuration — `config.json`

```json
{
  "data": {
    "contact": { "name": "...", "email": "...", "phone": "..." }
  },
  "workflow": {
    "id": "email-track-workflow",
    "task_queue": "approval-task-queue",
    "steps": [ ... ]
  }
}
```

### Step types

| Type | Required fields | Description |
|------|----------------|-------------|
| `wait` | `wait_duration` (e.g. `"1m"`) | Durable sleep |
| `sendEmail` | `properties.toEmail/subject/body`, `to` | Send an email |
| `waitForEvent` | `event_type`, `timeout`, `on_event`, `on_timeout` | Block on a Temporal signal |

Templates `{{contact.name}}`, `{{contact.email}}`, `{{contact.phone}}` are resolved in `properties` values.

## Prerequisites

- Go 1.24+
- Docker + Docker Compose

## Running

**Step 1 — Start the Temporal server:**
```bash
docker compose up -d
```
Temporal UI: http://localhost:8080

**Step 2 — Start the worker** (keep running in Terminal 1):
```bash
go run worker/main.go
```

**Step 3 — Trigger a workflow run** (Terminal 2):
```bash
go run trigger/main.go
```
Output:
```
Started workflow: ID=email-track-workflow-<uuid> RunID=<uuid>
```
The starter blocks waiting for the result. The workflow will:
1. Sleep for 1 minute
2. Send the initial email
3. Wait up to 2 minutes for an `email-open` signal

## Testing both branches

### Branch A — Signal received (email opened)

While the workflow is paused on `waitForEvent`, send the signal (Terminal 3):
```bash
go run signaler/main.go "email-track-workflow-<uuid>"
```

The workflow takes the `on_event` path and sends the "Thanks for opening!" email.

Expected result:
```
[step 0 wait] waited for 1m0s
[step 1 sendEmail] email sent to user@example.com — subject: "Hello from Temporal" — ...
[step 2 waitForEvent] received signal email-open: user clicked the link
[step 3 sendEmail] email sent to user@example.com — subject: "Thanks for opening!" — ...
```

### Branch B — Timeout (email not opened)

Don't send a signal. After 2 minutes the workflow takes the `on_timeout` path.

Expected result:
```
[step 0 wait] waited for 1m0s
[step 1 sendEmail] email sent to user@example.com — subject: "Hello from Temporal" — ...
[step 2 waitForEvent] timed out waiting for email-open
[step 4 sendEmail] email sent to user@example.com — subject: "We missed you" — ...
```

To test the timeout branch quickly, set `"timeout": "10s"` on the `waitForEvent` step in `config.json`.

## Running multiple workflows simultaneously

Each `go run trigger/main.go` starts an independent run with a unique ID. You can trigger several in parallel and send signals to specific runs by their printed ID.

## Key concepts

| Concept | Description |
|---------|-------------|
| **Workflow** | Durable orchestration logic — must be deterministic |
| **Activity** | The real work (I/O, external calls) — can be non-deterministic |
| **Signal** | External event sent to a running workflow (`workflow.GetSignalChannel`) |
| **workflow.Sleep** | Durable timer — survives worker restarts, no activity needed |
| **Worker** | Polls the task queue and executes workflows/activities |
| **Task Queue** | Named channel connecting triggers to workers |