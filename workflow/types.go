package workflow

import "encoding/json"

type WorkflowInput struct {
	Data     DataInput   `json:"data"`
	Workflow WorkflowDef `json:"workflow"`
}

// --- Data section ---

type DataInput struct {
	Contact ContactInfo `json:"contact"`
	Trigger TriggerInfo `json:"trigger"`
}

type ContactInfo struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}

type TriggerInfo struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// --- Workflow definition section ---

type WorkflowDef struct {
	ID        string `json:"id"`
	TaskQueue string `json:"task_queue"`
	Steps     []Step `json:"steps"`
}

// Step is one node in the workflow graph.
//
// From is json.RawMessage to accept both "trigger" (string) and an integer
// step ID — Go's JSON decoder cannot handle a field that is sometimes a string
// and sometimes a number without using RawMessage or interface{}.
//
// To, OnEvent, OnTimeout are *int so that JSON null (or a missing field)
// decodes to nil, which the workflow loop treats as "no next step" (terminal).
type Step struct {
	ID           int               `json:"id"`
	Type         string            `json:"type"`
	From         json.RawMessage   `json:"from"`
	To           *int              `json:"to"`
	OnEvent      *int              `json:"on_event"`
	OnTimeout    *int              `json:"on_timeout"`
	EventType    string            `json:"event_type"`
	Timeout      string            `json:"timeout"`       // e.g. "3s", "2m"
	WaitDuration string            `json:"wait_duration"` // e.g. "60m", "1h"
	Properties   map[string]string `json:"properties"`
}

// IsStartStep returns true when this step's From value is the JSON string
// "trigger". json.RawMessage stores raw bytes, so the string "trigger" is
// represented as the byte sequence `"trigger"` (with enclosing quotes).
func (s Step) IsStartStep() bool {
	return string(s.From) == `"trigger"`
}
