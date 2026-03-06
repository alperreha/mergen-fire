package firecracker

import (
	"log/slog"
	"time"
)

func NewConfigurator(timeout time.Duration, logger *slog.Logger) Configurator {
	return NewSDKConfigurator(timeout).WithLogger(logger)
}
