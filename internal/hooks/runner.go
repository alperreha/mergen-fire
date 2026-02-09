package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/alperreha/mergen-fire/internal/model"
)

type Runner struct {
	logger *slog.Logger
	client *http.Client
}

func NewRunner(logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		logger: logger,
		client: &http.Client{},
	}
}

func (r *Runner) RunAsync(event string, hooks []model.HookEntry, payload model.HookContext) {
	if len(hooks) == 0 {
		r.logger.Debug("no hooks to execute", "event", event, "vmID", payload.ID)
		return
	}
	r.logger.Debug("scheduling async hook execution", "event", event, "vmID", payload.ID, "hookCount", len(hooks))

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := r.Run(ctx, event, hooks, payload); err != nil {
			r.logger.Warn("hook execution finished with errors", "event", event, "vmID", payload.ID, "error", err)
			return
		}
		r.logger.Debug("hook execution finished", "event", event, "vmID", payload.ID, "hookCount", len(hooks))
	}()
}

func (r *Runner) Run(ctx context.Context, event string, hooks []model.HookEntry, payload model.HookContext) error {
	var strictErrors []error

	for i, hook := range hooks {
		r.logger.Debug("executing hook", "event", event, "vmID", payload.ID, "index", i, "type", hook.Type, "strict", hook.Strict)
		if err := r.execute(ctx, hook, payload); err != nil {
			r.logger.Warn("hook failed", "event", event, "type", hook.Type, "vmID", payload.ID, "error", err)
			if hook.Strict {
				strictErrors = append(strictErrors, err)
			}
			continue
		}
		r.logger.Debug("hook executed successfully", "event", event, "vmID", payload.ID, "index", i, "type", hook.Type)
	}

	if len(strictErrors) > 0 {
		return errors.Join(strictErrors...)
	}
	return nil
}

func (r *Runner) execute(ctx context.Context, hook model.HookEntry, payload model.HookContext) error {
	hookCtx := ctx
	cancel := func() {}
	if hook.TimeoutMs > 0 {
		hookCtx, cancel = context.WithTimeout(ctx, time.Duration(hook.TimeoutMs)*time.Millisecond)
	}
	defer cancel()

	switch strings.ToLower(strings.TrimSpace(hook.Type)) {
	case "http":
		return r.execHTTP(hookCtx, hook, payload)
	case "exec":
		return r.execCommand(hookCtx, hook, payload)
	default:
		return fmt.Errorf("unsupported hook type: %s", hook.Type)
	}
}

func (r *Runner) execHTTP(ctx context.Context, hook model.HookEntry, payload model.HookContext) error {
	if hook.URL == "" {
		return errors.New("http hook url is empty")
	}
	r.logger.Debug("executing http hook", "vmID", payload.ID, "url", hook.URL)
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range hook.Headers {
		req.Header.Set(key, value)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected response status: %s", resp.Status)
	}
	r.logger.Debug("http hook succeeded", "vmID", payload.ID, "url", hook.URL, "status", resp.Status)
	return nil
}

func (r *Runner) execCommand(ctx context.Context, hook model.HookEntry, payload model.HookContext) error {
	if len(hook.Cmd) == 0 {
		return errors.New("exec hook command is empty")
	}

	argv := make([]string, 0, len(hook.Cmd))
	for _, part := range hook.Cmd {
		rendered, err := renderTemplate(part, payload)
		if err != nil {
			return err
		}
		argv = append(argv, rendered)
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	r.logger.Debug("executing command hook", "vmID", payload.ID, "command", strings.Join(argv, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("exec hook failed: %w, output=%s", err, strings.TrimSpace(string(output)))
	}
	trimmed := strings.TrimSpace(string(output))
	if trimmed != "" {
		r.logger.Debug("command hook output", "vmID", payload.ID, "command", strings.Join(argv, " "), "output", trimmed)
	}
	return nil
}

func renderTemplate(input string, payload model.HookContext) (string, error) {
	tpl, err := template.New("hook").Option("missingkey=error").Parse(input)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, payload); err != nil {
		return "", err
	}
	return buf.String(), nil
}
