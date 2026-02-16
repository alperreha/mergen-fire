package main

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const defaultIntervalSeconds = 10

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	interval := readInterval()
	logger.Info("mergen telemetry started", "interval", interval.String())

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	signalCh := make(chan os.Signal, 2)
	signal.Notify(signalCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signalCh)

	publish(logger)
	for {
		select {
		case <-ticker.C:
			publish(logger)
		case sig := <-signalCh:
			logger.Info("mergen telemetry exiting", "signal", sig.String())
			return
		}
	}
}

func readInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("MERGEN_TELEMETRY_INTERVAL_SECONDS"))
	if raw == "" {
		return defaultIntervalSeconds * time.Second
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return defaultIntervalSeconds * time.Second
	}
	return time.Duration(seconds) * time.Second
}

func publish(logger *slog.Logger) {
	label := randomLabel(8)
	logger.Info(
		"mergen telemetry heartbeat",
		"time", time.Now().UTC().Format(time.RFC3339Nano),
		"test_label", label,
	)
}

func randomLabel(byteLen int) string {
	if byteLen <= 0 {
		byteLen = 8
	}
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "fallback-random"
	}
	return hex.EncodeToString(buf)
}
