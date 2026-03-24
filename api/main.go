package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
	"go.temporal.io/sdk/client"

	workflow "temporal-cart-flow/workflow"
)

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

func main() {
	c, err := client.Dial(client.Options{HostPort: "localhost:7233"})
	if err != nil {
		log.Fatalln("Unable to connect to Temporal:", err)
	}
	defer c.Close()

	s := &server{temporal: c}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /workflows", s.startWorkflow)
	mux.HandleFunc("GET /workflows/{id}", s.getWorkflow)
	mux.HandleFunc("POST /workflows/{id}/signal", s.signalWorkflow)

	log.Println("API server listening on :8081")
	if err := http.ListenAndServe(":8081", mux); err != nil {
		log.Fatalln(err)
	}
}
