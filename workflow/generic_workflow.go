package workflow

import (
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/workflow"

	"temporal-cart-flow/activities"
)

// GenericWorkflow interprets a JSON-defined step graph and executes each step
// as a Temporal activity. The step graph is defined in WorkflowInput.Workflow.Steps.
//
// Execution starts at the step with From == "trigger" and follows To / OnEvent /
// OnTimeout pointers until a nil pointer (terminal step) is reached.
func GenericWorkflow(ctx workflow.Context, input WorkflowInput) (string, error) {
	stepMap := buildStepMap(input.Workflow.Steps)
	current, err := findStartStep(input.Workflow.Steps)
	if err != nil {
		return "", err
	}

	contact := input.Data.Contact
	var results []string

	for {
		var stepResult string
		var nextID *int

		switch current.Type {

		case "sendEmail":
			props := resolveProperties(current.Properties, contact)
			actCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
				StartToCloseTimeout: 30 * time.Second,
			})
			var result string
			err = workflow.ExecuteActivity(actCtx, activities.SendEmailActivity,
				props["toEmail"], props["subject"], props["body"],
			).Get(ctx, &result)
			if err != nil {
				return "", fmt.Errorf("step %d sendEmail: %w", current.ID, err)
			}
			stepResult = result
			nextID = current.To

		case "wait":
			d, err := time.ParseDuration(current.WaitDuration)
			if err != nil {
				return "", fmt.Errorf("step %d wait: invalid wait_duration %q: %w", current.ID, current.WaitDuration, err)
			}
			if err = workflow.Sleep(ctx, d); err != nil {
				return "", fmt.Errorf("step %d wait: %w", current.ID, err)
			}
			stepResult = fmt.Sprintf("waited for %s", d)
			nextID = current.To

		case "waitForEvent":
			d, err := time.ParseDuration(current.Timeout)
			if err != nil {
				return "", fmt.Errorf("step %d waitForEvent: invalid timeout %q: %w", current.ID, current.Timeout, err)
			}
			var payload string
			received, _ := workflow.GetSignalChannel(ctx, current.EventType).ReceiveWithTimeout(ctx, d, &payload)
			if received {
				stepResult = fmt.Sprintf("received signal %s: %s", current.EventType, payload)
				nextID = current.OnEvent
			} else {
				stepResult = fmt.Sprintf("timed out waiting for %s", current.EventType)
				nextID = current.OnTimeout
			}

		default:
			return "", fmt.Errorf("step %d: unknown type %q", current.ID, current.Type)
		}

		results = append(results, fmt.Sprintf("[step %d %s] %s", current.ID, current.Type, stepResult))

		if nextID == nil {
			break
		}
		next, ok := stepMap[*nextID]
		if !ok {
			return "", fmt.Errorf("step %d references unknown next step %d", current.ID, *nextID)
		}
		current = next
	}

	return strings.Join(results, "\n"), nil
}

// buildStepMap indexes steps by ID for O(1) lookup during execution.
func buildStepMap(steps []Step) map[int]Step {
	m := make(map[int]Step, len(steps))
	for _, s := range steps {
		m[s.ID] = s
	}
	return m
}

// findStartStep returns the step whose From field is the string "trigger".
func findStartStep(steps []Step) (Step, error) {
	for _, s := range steps {
		if s.IsStartStep() {
			return s, nil
		}
	}
	return Step{}, fmt.Errorf("no start step found (no step with from: \"trigger\")")
}

// resolveTemplate replaces {{contact.*}} placeholders with values from ContactInfo.
func resolveTemplate(s string, c ContactInfo) string {
	s = strings.ReplaceAll(s, "{{contact.name}}", c.Name)
	s = strings.ReplaceAll(s, "{{contact.email}}", c.Email)
	s = strings.ReplaceAll(s, "{{contact.phone}}", c.Phone)
	return s
}

// resolveProperties applies resolveTemplate to every value in a properties map.
func resolveProperties(props map[string]string, c ContactInfo) map[string]string {
	resolved := make(map[string]string, len(props))
	for k, v := range props {
		resolved[k] = resolveTemplate(v, c)
	}
	return resolved
}
