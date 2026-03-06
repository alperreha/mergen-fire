package hooks

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type Handler func(context.Context, Request) error

type Definition struct {
	Name   string
	Strict bool
	Handle Handler
}

type Manager struct {
	stages map[string][]Definition
}

func NewManager() *Manager {
	return &Manager{stages: make(map[string][]Definition)}
}

func (m *Manager) Register(stage string, defs ...Definition) {
	stageKey := normalizeStage(stage)
	if stageKey == "" || len(defs) == 0 {
		return
	}
	m.stages[stageKey] = append(m.stages[stageKey], defs...)
}

func (m *Manager) Definitions(stage string) []Definition {
	stageKey := normalizeStage(stage)
	defs := m.stages[stageKey]
	if len(defs) == 0 {
		return nil
	}
	out := make([]Definition, len(defs))
	copy(out, defs)
	return out
}

func (m *Manager) Run(ctx context.Context, req Request, defs []Definition) error {
	for _, def := range defs {
		if def.Handle == nil {
			continue
		}

		if err := runDefinition(ctx, req, def); err != nil {
			name := strings.TrimSpace(def.Name)
			if name == "" {
				name = "unnamed-hook"
			}
			if def.Strict {
				return fmt.Errorf("%s: %w", name, err)
			}
			if req.Logger != nil {
				req.Logger.Warn("non-strict lifecycle hook failed", "vmID", req.VMID, "stage", req.Stage, "hook", name, "error", err)
			}
		}
	}
	return nil
}

func runDefinition(ctx context.Context, req Request, def Definition) error {
	var (
		wg    sync.WaitGroup
		errCh = make(chan error, 1)
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- def.Handle(ctx, req)
	}()

	wg.Wait()
	return <-errCh
}

func normalizeStage(stage string) string {
	return strings.TrimSpace(stage)
}
