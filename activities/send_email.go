package activities

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/activity"
)

// SendEmailActivity simulates sending an email with a 2-second delivery delay.
func SendEmailActivity(ctx context.Context, to, subject, body string) (string, error) {
	activity.GetLogger(ctx).Info("Sending email", "to", to, "subject", subject)
	time.Sleep(2 * time.Second)
	return fmt.Sprintf("email sent to %s — subject: %q — body: %s", to, subject, body), nil
}
