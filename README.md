# temporal-basics

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
temporal-basics/
├── config.json                 # Step graph + contact data (read by trigger, worker, api)
├── workflow/
│   ├── generic_workflow.go     # GenericWorkflow — interprets the step graph
│   └── types.go                # WorkflowInput, Step, ContactInfo, etc.
├── activities/
│   └── send_email.go           # SendEmailActivity — 2s fake delay
├── worker/
│   └── main.go                 # Starts the Temporal worker
├── trigger/
│   └── main.go                 # CLI: starts a new workflow run directly via SDK
├── signaler/
│   └── main.go                 # CLI: sends an "email-open" signal to a running workflow
├── api/
│   └── main.go                 # HTTP REST API — trigger/signal/status + Kafka publish
├── kafka-consumer/
│   └── main.go                 # Kafka bridge — reads topics, triggers workflows / sends signals
├── proto/
│   └── Temporal-POC.postman_collection.json  # Postman collection for the HTTP API
├── docker-compose.yml                    # Recommended: Cassandra + OpenSearch + Kafka
├── docker-compose-dev.yml                # Local dev: same stack, built from source
├── docker-compose-cassandra.yml          # Lightweight: Cassandra only, no search backend
├── docker-compose-cassandra-elastic.yml  # Cassandra + Elasticsearch (ELv2 license)
└── docker-compose-postgresql.yml         # PostgreSQL instead of Cassandra
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

## Environment variables

| Variable | Service | Default | Purpose |
|----------|---------|---------|---------|
| `TEMPORAL_HOST` | worker, api, kafka-consumer | `localhost:7233` | Temporal server gRPC endpoint |
| `KAFKA_BROKER` | api, kafka-consumer | `localhost:9092` | Kafka broker address |

When running via `docker-compose.yml` these are pre-configured. Set them manually only when running services outside Docker.

## Quick check

The fastest way to verify the full flow end-to-end:

**1. Start the stack** (includes the worker, api, and kafka-consumer)
```bash
docker compose up -d
```

**2. Use the Postman collection** (`proto/Temporal-POC.postman_collection.json`) to drive the workflow:

| Request | Method | What it does |
|---------|--------|-------------|
| **Trigger** | `POST /workflows` | Start a new workflow; saves `workflow_id` and `run_id` as collection variables |
| **Fetch Info** | `GET /workflows/{{workflow_id}}` | Poll current status and step output |
| **Send Signal** | `POST /workflows/{{workflow_id}}/signal` | Fire the `email-open` signal, unblocking `waitForEvent` |
| **kafka Trigger** | `POST /kafka` | Publish a workflow-start message to Kafka; kafka-consumer picks it up and starts the workflow |
| **kafka Signal** | `POST /kafka` | Publish a signal message to Kafka; kafka-consumer forwards it to the running workflow |

**3. Check the Temporal UI**

Open [http://localhost:8080](http://localhost:8080) to see live workflow state, history, and step-by-step execution.

**4. Check the Kafka UI**

Open [http://localhost:8082](http://localhost:8082) to inspect topics, consumer groups, and message offsets.

---

## Infrastructure options

### Component licensing & trade-offs

| Technology | License | Who Controls | Self-Host Cost | Managed Options | Benefits | Problems / Risks |
|---|---|---|---|---|---|---|
| **PostgreSQL** | PostgreSQL License (MIT-like) | PostgreSQL Global Dev Group | Free | AWS RDS, Cloud SQL, Neon, Supabase, Aiven | Truly permissive; ACID; widely understood; Temporal advanced visibility built-in (v1.20+); no ES needed; huge managed hosting market | Not built for extreme Temporal write scale; needs PgBouncer for connection pooling; vertical scaling limits |
| **Cassandra** | Apache 2.0 | Apache Foundation | Free | AWS Keyspaces, DataStax Astra, Instaclustr | Horizontally scalable; multi-region active-active; proven at massive scale (Meta, Netflix, Apple); no SPOF | Operationally complex — compaction, repair jobs, tuning; eventual consistency needs care; needs ES/OpenSearch for advanced Temporal visibility; overkill for small teams |
| **ScyllaDB** | AGPL v3 (OSS) / Commercial | ScyllaDB Inc. | Free (OSS) | ScyllaDB Cloud (paid) | Drop-in Cassandra replacement written in C++ — 10x lower latency, far fewer nodes needed; same CQL API; Temporal compatible | AGPL — modifications exposed as a service must be open-sourced; commercial license needed for proprietary SaaS modifications; smaller community than Cassandra |
| **Elasticsearch ≥ v7.11** | SSPL v1 | Elastic NV | Free to run | Elastic Cloud | Mature, battle-tested; best-in-class full-text search; deep Temporal advanced visibility support; large ecosystem | **SSPL blocks commercial SaaS use** — must open-source your whole stack or buy Elastic commercial license; Elastic can change terms again |
| **Elasticsearch ≤ v7.10** | Apache 2.0 | Elastic NV (frozen) | Free | — | Permissive; no copyleft | **EOL — no security patches, no new features**; running in production is a security liability; avoid for new projects |
| **OpenSearch** | Apache 2.0 | AWS / OpenSearch Foundation | Free | AWS OpenSearch Service, Aiven, Bonsai | **Best ES alternative** — permissive license; API-compatible with ES 7.10; actively maintained; Temporal officially supports it; no Elastic vendor lock-in | Lags behind ES on some advanced features; AWS-driven roadmap; smaller community than ES |
| **Temporal Server** | MIT | Temporal Technologies | Free | Temporal Cloud (usage-based) | Fully open; no restrictions — fork, embed, sell; large and growing community | You own all ops: upgrades, schema migrations, monitoring, multi-region; requires a persistence + visibility backend |
| **Temporal UI** | MIT | Temporal Technologies | Free | Bundled with Temporal Cloud | Clean workflow visibility; timeline views; search by attributes; open source and customisable | Advanced search features need Elasticsearch, OpenSearch, or PostgreSQL (v1.20+) |
| **Kafka** | Apache 2.0 | Apache Foundation | Free | Confluent Cloud, AWS MSK, Aiven, Redpanda | De-facto standard for event streaming; durable log; replay capability; high throughput; huge ecosystem | Operationally heavy; complex tuning (partitions, retention, replication factor); not a queue — requires careful consumer group design |
| **Kafka UI** | Apache 2.0 | Provectus (community) | Free | — | Clean open-source UI; topic browser, consumer lag, message inspector, schema registry support | Community-maintained — less polished than Confluent Control Center; no commercial backing or SLA |

### License risk summary

| Technology | Safe for Internal Use | Safe for Commercial SaaS | Key Condition |
|---|---|---|---|
| PostgreSQL | Yes | Yes | No restrictions |
| Cassandra | Yes | Yes | Apache 2.0 — no restrictions |
| ScyllaDB (OSS) | Yes | **Risky** | AGPL — modifications exposed as a service must be open-sourced |
| ScyllaDB (Commercial) | Yes | Yes | Requires paid license |
| Elasticsearch ≥ 7.11 | Yes | **No** | SSPL blocks SaaS use without commercial license |
| Elasticsearch ≤ 7.10 | Technically yes | Technically yes | Apache 2.0 — but **EOL, avoid** |
| OpenSearch | Yes | Yes | Apache 2.0 — no restrictions |
| Temporal Server | Yes | Yes | MIT — no restrictions |
| Temporal UI | Yes | Yes | MIT — no restrictions |
| Kafka | Yes | Yes | Apache 2.0 — no restrictions |
| Kafka UI | Yes | Yes | Apache 2.0 — no restrictions |

### Recommended combinations

| Scale / Scenario | Persistence | Visibility | Streaming | License Concern |
|---|---|---|---|---|
| **Small team / startup** | PostgreSQL | PostgreSQL (built-in) | Kafka | None |
| **Mid-scale, no ES** | PostgreSQL | PostgreSQL (built-in) | Kafka | None |
| **High scale, clean license** | Cassandra | OpenSearch | Kafka | None |
| **High scale, cost-sensitive** | ScyllaDB (OSS, not SaaS) | OpenSearch | Kafka | AGPL on ScyllaDB modifications |
| **Maximum scale, all in** | ScyllaDB (commercial) | Elasticsearch (commercial) | Kafka | Paid licenses required |
| **Fully managed, no ops** | Temporal Cloud | Temporal Cloud (built-in) | Confluent Cloud | Usage-based cost |

---

### Docker Compose files — pick one based on your needs

| File | Persistence | Visibility | License | Notes |
|------|------------|------------|---------|-------|
| `docker-compose.yml` | Cassandra 4.1 | OpenSearch 2.13 | Apache 2.0 | **Recommended** — production-grade, fully open |
| `docker-compose-dev.yml` | Cassandra 4.1 | OpenSearch 2.13 | Apache 2.0 | Same as above but builds from local source |
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

### Local dev (build from source)

```bash
docker compose -f docker-compose-dev.yml up -d
```

---

## Running

`docker compose up -d` starts **all** services including the worker, API, and kafka-consumer. Use the manual steps below only when you want to run services outside Docker (e.g. for live code reloading during development).

**Step 1 — Start the Temporal server:**
```bash
docker compose up -d
```
Temporal UI: http://localhost:8080  
Kafka UI: http://localhost:8082

**Step 2 — Start the worker** (keep running in Terminal 1):
```bash
go run worker/main.go
```

**Step 3 — Start the API server** (keep running in Terminal 2):
```bash
go run api/main.go
# Listens on :8081
```

**Step 4 — Start the Kafka consumer** (keep running in Terminal 3):
```bash
go run kafka-consumer/main.go
# Subscribes to temporal.workflow.start and temporal.workflow.signal
```

**Step 5 — Trigger a workflow run** (Terminal 4):
```bash
go run trigger/main.go
```
Output:
```
Started workflow: ID=email-track-workflow-<uuid> RunID=<uuid>
```
The starter blocks waiting for the result. The workflow will:
1. Sleep for the configured wait duration
2. Send the initial email
3. Wait up to the configured timeout for an `email-open` signal

## API endpoints

The API server (`api/main.go`) listens on `:8081`.

| Method | Path | Body | Response | Description |
|--------|------|------|----------|-------------|
| `POST` | `/workflows` | `WorkflowInput` JSON | `202 {"workflow_id":"…","run_id":"…"}` | Start a new workflow |
| `GET` | `/workflows/{id}` | — | `200 {"status":"…","run_id":"…"}` | Get workflow status |
| `POST` | `/workflows/{id}/signal` | `{"signal_name":"…","payload":"…"}` | `200 {"ok":true}` | Send a signal to a running workflow |
| `POST` | `/kafka` | `{"topic":"…","payload":{…}}` | `202 {"status":"published"}` | Publish a message to Kafka |

Valid Kafka topics: `temporal.workflow.start`, `temporal.workflow.signal`.

## Kafka integration

Two Kafka topics bridge async events into Temporal:

| Topic | Consumer group | Message format | Effect |
|-------|---------------|----------------|--------|
| `temporal.workflow.start` | `temporal-consumer-start` | `{"input": <WorkflowInput>}` | Starts a new workflow via the Temporal SDK |
| `temporal.workflow.signal` | `temporal-consumer-signal` | `{"workflow_id":"…","signal_name":"…","payload":"…"}` | Sends a signal to an existing workflow |

The kafka-consumer service:
- Runs both consumers concurrently
- Auto-creates topics on startup if they don't exist
- Commits offsets only after successful Temporal calls
- Shuts down gracefully on SIGTERM/SIGINT

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

Don't send a signal. After the configured timeout the workflow takes the `on_timeout` path.

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

## Deploy Docker Image
```
export temporal_poc_version="1.1.0"
docker build . -t riteshbxr/temporal-poc:$temporal_poc_version
docker tag riteshbxr/temporal-poc:$temporal_poc_version riteshbxr/temporal-poc:latest
docker push riteshbxr/temporal-poc:latest
docker push riteshbxr/temporal-poc:$temporal_poc_version
```
