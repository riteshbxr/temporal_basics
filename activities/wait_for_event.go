package activities

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/activity"
)

// EventResult is the outcome of WaitForEventActivity.
type EventResult struct {
	Received bool
	Message  string
}

// WaitForEventActivity simulates polling an external API/gRPC endpoint for an event.
// The simulated event always fires after 2 seconds.
//   - eventTimeout > 2s  →  Received = true  (event arrived in time)
//   - eventTimeout < 2s  →  Received = false (timed out)
func WaitForEventActivity(ctx context.Context, eventID string, eventTimeout time.Duration) (EventResult, error) {
	activity.GetLogger(ctx).Info("Polling for event", "eventID", eventID, "timeout", eventTimeout)

	eventCh := make(chan string, 1)
	go func() {
		// Simulate the external service firing after 2 seconds.
		select {
		case <-time.After(2 * time.Second):
			eventCh <- fmt.Sprintf("approval data for %s", eventID)
		case <-ctx.Done():
		}
	}()

	eventCtx, cancel := context.WithTimeout(ctx, eventTimeout)
	defer cancel()

	select {
	case data := <-eventCh:
		return EventResult{Received: true, Message: data}, nil
	case <-eventCtx.Done():
		return EventResult{Received: false, Message: fmt.Sprintf("timed out waiting for event %s", eventID)}, nil
	}
}
