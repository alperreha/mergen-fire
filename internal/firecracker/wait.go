package firecracker

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const defaultSocketPollInterval = 200 * time.Millisecond

func WaitForSocket(ctx context.Context, socketPath string, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		pollInterval = defaultSocketPollInterval
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		present, err := SocketPresent(socketPath)
		if err != nil {
			return err
		}
		if present {
			return nil
		}

		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("socket not available before timeout: %s", socketPath)
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
