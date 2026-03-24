package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/google/uuid"
	kafka "github.com/segmentio/kafka-go"
	"go.temporal.io/sdk/client"

	workflow "temporal-cart-flow/workflow"
)

const (
	topicStart = "temporal.workflow.start"
	topicSignal = "temporal.workflow.signal"
)

var kafkaBroker = envOrDefault("KAFKA_BROKER", "localhost:9092")

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

type server struct {
	temporal client.Client
}

type signalRequest struct {
	SignalName string `json:"signal_name"`
	Payload    string `json:"payload"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// POST /workflows
// Body: full WorkflowInput JSON
// Response 202: {"workflow_id": "...", "run_id": "..."}
func (s *server) startWorkflow(w http.ResponseWriter, r *http.Request) {
	var input workflow.WorkflowInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if input.Workflow.TaskQueue == "" {
		writeError(w, http.StatusBadRequest, "workflow.task_queue is required")
		return
	}

	workflowID := input.Workflow.ID + "-" + uuid.New().String()
	we, err := s.temporal.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: input.Workflow.TaskQueue,
	}, workflow.GenericWorkflow, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start workflow: "+err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"workflow_id": we.GetID(),
		"run_id":      we.GetRunID(),
	})
}

// GET /workflows/{id}
// Response 200: {"workflow_id": "...", "status": "...", "close_time": "..."}
func (s *server) getWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing workflow id")
		return
	}

	resp, err := s.temporal.DescribeWorkflowExecution(r.Context(), id, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to describe workflow: "+err.Error())
		return
	}

	info := resp.WorkflowExecutionInfo
	result := map[string]string{
		"workflow_id": info.Execution.WorkflowId,
		"run_id":      info.Execution.RunId,
		"status":      info.Status.String(),
	}
	if info.CloseTime != nil {
		result["close_time"] = info.CloseTime.AsTime().String()
	}

	writeJSON(w, http.StatusOK, result)
}

// POST /workflows/{id}/signal
// Body: {"signal_name": "...", "payload": "..."}
// Response 200: {"ok": true}
func (s *server) signalWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing workflow id")
		return
	}

	var req signalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.SignalName == "" {
		writeError(w, http.StatusBadRequest, "signal_name is required")
		return
	}

	err := s.temporal.SignalWorkflow(r.Context(), id, "", req.SignalName, req.Payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to send signal: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type kafkaRequest struct {
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
}

// POST /kafka
// Body: {"topic": "temporal.workflow.start", "payload": {...}}
// Publishes the raw payload to the given Kafka topic.
func (s *server) kafkaPublishMessage(w http.ResponseWriter, r *http.Request) {
	var req kafkaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Topic != topicStart && req.Topic != topicSignal {
		writeError(w, http.StatusBadRequest, "topic must be one of: "+topicStart+", "+topicSignal)
		return
	}
	if len(req.Payload) == 0 {
		writeError(w, http.StatusBadRequest, "payload is required")
		return
	}

	if err := kafkaPublish(r.Context(), req.Topic, req.Payload); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to publish: "+err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"topic":  req.Topic,
		"status": "queued",
	})
}

func kafkaPublish(ctx context.Context, topic string, value []byte) error {
	w := &kafka.Writer{
		Addr:                   kafka.TCP(kafkaBroker),
		Topic:                  topic,
		AllowAutoTopicCreation: true,
	}
	defer w.Close()
	return w.WriteMessages(ctx, kafka.Message{Value: value})
}

func main() {
	c, err := client.Dial(client.Options{HostPort: envOrDefault("TEMPORAL_HOST", "localhost:7233")})
	if err != nil {
		log.Fatalln("Unable to connect to Temporal:", err)
	}
	defer c.Close()

	s := &server{temporal: c}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /workflows", s.startWorkflow)
	mux.HandleFunc("GET /workflows/{id}", s.getWorkflow)
	mux.HandleFunc("POST /workflows/{id}/signal", s.signalWorkflow)

	mux.HandleFunc("POST /kafka", s.kafkaPublishMessage)

	log.Println("API server listening on :8081")
	if err := http.ListenAndServe(":8081", mux); err != nil {
		log.Fatalln(err)
	}
}
