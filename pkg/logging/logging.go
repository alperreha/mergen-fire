package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

func New(level, format string) *slog.Logger {
	minLevel := parseLevel(level)
	format = strings.ToLower(strings.TrimSpace(format))

	writer := io.Writer(os.Stdout)
	switch format {
	case "json":
		return slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: minLevel}))
	case "text":
		return slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: minLevel}))
	default:
		return slog.New(newConsoleHandler(writer, minLevel))
	}
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

type consoleHandler struct {
	mu       sync.Mutex
	w        io.Writer
	minLevel slog.Level
	attrs    []slog.Attr
	groups   []string
}

func newConsoleHandler(w io.Writer, minLevel slog.Level) *consoleHandler {
	return &consoleHandler{
		w:        w,
		minLevel: minLevel,
		attrs:    nil,
		groups:   nil,
	}
}

func (h *consoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

func (h *consoleHandler) Handle(_ context.Context, record slog.Record) error {
	var builder strings.Builder

	levelText := strings.ToUpper(record.Level.String())
	levelColor := colorForLevel(record.Level)
	builder.WriteString(levelColor)
	builder.WriteString("[")
	builder.WriteString(levelText)
	builder.WriteString("]")
	builder.WriteString(colorReset)
	builder.WriteString(" ")
	builder.WriteString(record.Time.UTC().Format(time.RFC3339))
	builder.WriteString(" ")
	builder.WriteString(record.Message)

	merged := make([]slog.Attr, 0, len(h.attrs)+record.NumAttrs())
	merged = append(merged, h.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		merged = append(merged, attr)
		return true
	})

	for _, attr := range merged {
		appendAttr(&builder, attr, h.groups)
	}
	builder.WriteString("\n")

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, builder.String())
	return err
}

func (h *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &next
}

func (h *consoleHandler) WithGroup(name string) slog.Handler {
	if strings.TrimSpace(name) == "" {
		return h
	}
	next := *h
	next.groups = append(append([]string(nil), h.groups...), name)
	return &next
}

func appendAttr(builder *strings.Builder, attr slog.Attr, groups []string) {
	attr.Value = attr.Value.Resolve()

	if attr.Value.Kind() == slog.KindGroup {
		groupName := attr.Key
		nextGroups := groups
		if groupName != "" {
			nextGroups = append(append([]string(nil), groups...), groupName)
		}
		for _, nested := range attr.Value.Group() {
			appendAttr(builder, nested, nextGroups)
		}
		return
	}

	keyParts := make([]string, 0, len(groups)+1)
	keyParts = append(keyParts, groups...)
	if attr.Key != "" {
		keyParts = append(keyParts, attr.Key)
	}
	key := strings.Join(keyParts, ".")
	if key == "" {
		return
	}

	builder.WriteString(" ")
	builder.WriteString(key)
	builder.WriteString("=")
	builder.WriteString(formatValue(attr.Value))
}

func formatValue(value slog.Value) string {
	value = value.Resolve()
	switch value.Kind() {
	case slog.KindString:
		text := value.String()
		if text == "" {
			return `""`
		}
		if strings.ContainsAny(text, " \t\n\"=") {
			return strconv.Quote(text)
		}
		return text
	case slog.KindBool:
		if value.Bool() {
			return "true"
		}
		return "false"
	case slog.KindInt64:
		return strconv.FormatInt(value.Int64(), 10)
	case slog.KindUint64:
		return strconv.FormatUint(value.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.FormatFloat(value.Float64(), 'f', -1, 64)
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindTime:
		return value.Time().UTC().Format(time.RFC3339)
	case slog.KindAny:
		return fmt.Sprintf("%v", value.Any())
	default:
		return value.String()
	}
}

func colorForLevel(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return colorRed
	case level >= slog.LevelWarn:
		return colorYellow
	case level >= slog.LevelInfo:
		return colorBlue
	default:
		return colorCyan
	}
}

const (
	colorReset  = "\033[0m"
	colorBlue   = "\033[34m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorCyan   = "\033[36m"
)
