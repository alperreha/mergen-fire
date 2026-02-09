package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

func New(level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: parseLevel(level),
	}

	writer := io.Writer(os.Stdout)
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "text" {
		return slog.New(slog.NewTextHandler(writer, opts))
	}
	return slog.New(slog.NewJSONHandler(writer, opts))
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
