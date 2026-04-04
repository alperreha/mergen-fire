package images

import (
	"fmt"
	"io"
	"sync"
	"time"
)

type ProgressUpdate struct {
	Kind             string    `json:"kind"`
	Name             string    `json:"name"`
	FileName         string    `json:"fileName"`
	LocalPath        string    `json:"localPath,omitempty"`
	RemoteKey        string    `json:"remoteKey,omitempty"`
	Direction        string    `json:"direction"`
	Status           string    `json:"status"`
	TransferredBytes int64     `json:"transferredBytes"`
	TotalBytes       int64     `json:"totalBytes,omitempty"`
	Percent          float64   `json:"percent,omitempty"`
	StartedAt        time.Time `json:"startedAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
	Message          string    `json:"message,omitempty"`
}

type ProgressReporter interface {
	Report(ProgressUpdate)
}

type ProgressReporterFunc func(ProgressUpdate)

func (f ProgressReporterFunc) Report(update ProgressUpdate) {
	if f != nil {
		f(update)
	}
}

type DiscardReporter struct{}

func (DiscardReporter) Report(ProgressUpdate) {}

type ChannelReporter struct {
	C chan ProgressUpdate
}

func (r ChannelReporter) Report(update ProgressUpdate) {
	if r.C == nil {
		return
	}
	select {
	case r.C <- update:
	default:
	}
}

type CLIReporter struct {
	w     io.Writer
	mu    sync.Mutex
	lines map[string]bool
}

func NewCLIReporter(w io.Writer) *CLIReporter {
	return &CLIReporter{
		w:     w,
		lines: make(map[string]bool),
	}
}

func (r *CLIReporter) Report(update ProgressUpdate) {
	if r == nil || r.w == nil {
		return
	}

	key := update.Direction + "|" + update.FileName
	line := fmt.Sprintf(
		"%s %-8s %-24s %s",
		update.Direction,
		update.Status,
		update.FileName,
		formatProgress(update.TransferredBytes, update.TotalBytes),
	)

	r.mu.Lock()
	defer r.mu.Unlock()

	if update.Status == "running" {
		_, _ = fmt.Fprintf(r.w, "\r%s", line)
		r.lines[key] = true
		return
	}

	if r.lines[key] {
		_, _ = fmt.Fprint(r.w, "\r")
	}
	_, _ = fmt.Fprintln(r.w, line)
	delete(r.lines, key)
}

func formatProgress(transferred, total int64) string {
	if total <= 0 {
		return fmt.Sprintf("%s transferred", humanSize(transferred))
	}
	percent := float64(transferred) * 100 / float64(total)
	return fmt.Sprintf("%s / %s (%.1f%%)", humanSize(transferred), humanSize(total), percent)
}

func humanSize(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	unit := []string{"KiB", "MiB", "GiB", "TiB"}
	size := float64(value)
	idx := 0
	for size >= 1024 && idx < len(unit)-1 {
		size /= 1024
		idx++
	}
	return fmt.Sprintf("%.1f %s", size, unit[idx])
}
