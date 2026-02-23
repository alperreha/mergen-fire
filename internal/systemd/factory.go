package systemd

import (
	"log/slog"
	"time"
)

func NewClient(systemctlPath, unitPrefix string, timeout time.Duration, logger *slog.Logger) Client {
	if logger == nil {
		logger = slog.Default()
	}

	dbusClient := NewDBusClient(unitPrefix, timeout, logger.With("backend", "dbus"))
	if dbusClient.available {
		return dbusClient
	}

	logger.Warn("falling back to systemctl exec backend")
	return NewExecClient(systemctlPath, unitPrefix, timeout, logger.With("backend", "exec"))
}
