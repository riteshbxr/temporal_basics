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

## Quick check

The fastest way to verify the full flow end-to-end:

**1. Start the stack** (includes the worker)
```bash
docker compose up -d
```

**2. Use the Postman collection** (`proto/Temporal.postman_collection.json`) to drive the workflow:

| Request | What it does |
|---------|-------------|
| **Trigger** | `POST /workflows` — starts a new workflow run; automatically saves `workflow_id` and `run_id` to collection variables |
| **Fetch Info** | `GET /workflows/{{workflow_id}}` — poll current status and step output |
| **Send Signal** | `POST /workflows/{{workflow_id}}/signal` — fires the `email-open` signal, unblocking `waitForEvent` |

**3. Check the Temporal UI**

Open [http://localhost:8080](http://localhost:8080) to see live workflow state, history, and step-by-step execution in the Temporal dashboard.

---

## Infrastructure options

Four compose files are provided. Pick one based on your needs.

| File | Persistence | Visibility | License | Notes |
|------|------------|------------|---------|-------|
| `docker-compose.yml` | Cassandra 4.1 | OpenSearch 2.13 | Apache 2.0 | **Recommended** — production-grade, fully open |
| `docker-compose-cassandra.yml` | Cassandra 4.1 | Cassandra (default) | Apache 2.0 | Lightweight, no search backend |
| `docker-compose-cassandra-elastic.yml` | Cassandra 4.1 | Elasticsearch 8.13 | ELv2 ⚠ | Cannot be offered as a managed service |
| `docker-compose-postgresql.yml` | PostgreSQL 13 | PostgreSQL (default) | Apache 2.0 | Simpler ops if you already run Postgres |

> **Visibility** controls workflow search in the Temporal UI (filter by status, start time, workflow type, etc.).
> Without a dedicated search backend (OpenSearch/Elasticsearch) only basic filtering is available.

### Cassandra + OpenSearch (recommended)

```bash
docker compose up -d                          # uses docker-compose.yml by default
```

### Cassandra only

```bash
docker compose -f docker-compose-cassandra.yml up -d
```

### Cassandra + Elasticsearch

```bash
docker compose -f docker-compose-cassandra-elastic.yml up -d
```

### PostgreSQL only

```bash
docker compose -f docker-compose-postgresql.yml up -d
```

---

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

---

## Kubernetes Deployment Guidelines

### Data Loss Risk

The `docker-compose.yml` has **no volume mounts** — all data lives in ephemeral container storage. On Kubernetes, pods can restart or reschedule at any time, wiping all state. You must use PersistentVolumeClaims (PVCs) for every stateful service.

### Service Classification

| Service | Kind | PVC Needed | Replicas for HA |
|---|---|---|---|
| Cassandra | StatefulSet | Yes (50Gi+) | 3 |
| OpenSearch | StatefulSet | Yes (20Gi+) | 2+ |
| Kafka | StatefulSet | Yes (20Gi+) | 3 |
| Temporal server | Deployment | No | 2+ |
| api / worker / kafka-consumer | Deployment | No | 2+ |

### Cassandra (most critical — Temporal's primary store)

Run as a StatefulSet with a `volumeClaimTemplate`:

```yaml
volumeClaimTemplates:
  - metadata:
      name: cassandra-data
    spec:
      accessModes: ["ReadWriteOnce"]
      storageClassName: "gp2"   # adjust for your cloud provider
      resources:
        requests:
          storage: 50Gi
```

`temporal auto-setup` creates the Cassandra keyspace with replication factor 1 by default. For production, use RF=3 across 3 Cassandra nodes.

### OpenSearch (Temporal visibility store)

Same StatefulSet + PVC pattern. Set replicas and shard replication:

```yaml
- number_of_replicas: 1   # requires 2+ nodes
```

### Kafka

Mount the Kafka data directory via a `volumeClaimTemplate` and set `KAFKA_LOG_DIRS` to point to it. For durability with multiple brokers:

```yaml
- KAFKA_DEFAULT_REPLICATION_FACTOR=3
- KAFKA_MIN_INSYNC_REPLICAS=2   # producer must ack 2 replicas before success
```

A single-broker setup (`docker-compose.yml`) cannot survive pod loss without data loss regardless of PVCs.

### Temporal Server, API, Worker, Kafka Consumer

These are stateless — no storage needed. Use `Deployment` with `replicas: 2+` and a `readinessProbe` so Kubernetes only routes traffic to ready pods.

### Recommended Simplification for Production

Replace self-managed Cassandra and OpenSearch with managed equivalents:

- **Cassandra → managed PostgreSQL** (RDS, Cloud SQL) — a `docker-compose-postgresql.yml` variant is already provided
- **OpenSearch → managed OpenSearch Service** (AWS, GCP)

This eliminates StatefulSet complexity for the two most critical stores. You then only need to manage Kafka persistence yourself.

---

## Key concepts

| Concept | Description |
|---------|-------------|
| **Workflow** | Durable orchestration logic — must be deterministic |
| **Activity** | The real work (I/O, external calls) — can be non-deterministic |
| **Signal** | External event sent to a running workflow (`workflow.GetSignalChannel`) |
| **workflow.Sleep** | Durable timer — survives worker restarts, no activity needed |
| **Worker** | Polls the task queue and executes workflows/activities |
| **Task Queue** | Named channel connecting triggers to workers |