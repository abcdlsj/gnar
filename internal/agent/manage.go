package agent

import (
	"context"
	"fmt"
	"time"
)

func WaitForDaemon(ctx context.Context, daemonURL string) error {
	client := NewDaemonClient(daemonURL)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := client.Health(ctx); err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func daemonUnavailableHelp(daemonURL string) error {
	return fmt.Errorf("daemon not reachable at %s; start it with `gnar agent serve`", daemonURL)
}
