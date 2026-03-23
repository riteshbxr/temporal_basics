package activities

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/activity"
)

// WaitActivity pauses execution for the given duration.
func WaitActivity(ctx context.Context, duration time.Duration) (string, error) {
	activity.GetLogger(ctx).Info("Waiting", "duration", duration)
	time.Sleep(duration)
	return fmt.Sprintf("waited for %s", duration), nil
}
