package xdscenter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type ConsulPublisher struct {
	baseURL string
	token   string
	prefix  string
	client  *http.Client
	logger  *slog.Logger
}

func NewConsulPublisher(baseURL, token, prefix string, timeout time.Duration, logger *slog.Logger) *ConsulPublisher {
	if logger == nil {
		logger = slog.Default()
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &ConsulPublisher{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		prefix:  strings.Trim(strings.TrimSpace(prefix), "/"),
		client: &http.Client{
			Timeout: timeout,
		},
		logger: logger,
	}
}

func (p *ConsulPublisher) Enabled() bool {
	return p.baseURL != ""
}

func (p *ConsulPublisher) Prefix() string {
	return p.prefix
}

func (p *ConsulPublisher) SyncRoutes(ctx context.Context, routes []RouteRecord) (int, error) {
	if !p.Enabled() {
		return 0, nil
	}

	written := 0
	for _, route := range routes {
		if err := p.putRoute(ctx, route); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

func (p *ConsulPublisher) putRoute(ctx context.Context, route RouteRecord) error {
	payload, err := json.Marshal(route)
	if err != nil {
		return err
	}

	key := route.Host
	if p.prefix != "" {
		key = p.prefix + "/" + route.Host
	}
	url := fmt.Sprintf("%s/v1/kv/%s", p.baseURL, key)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	if p.token != "" {
		req.Header.Set("X-Consul-Token", p.token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("consul kv write failed status=%d key=%s body=%q", resp.StatusCode, key, strings.TrimSpace(string(body)))
	}
	p.logger.Debug("consul route synced", "key", key, "host", route.Host, "vmID", route.VMID)
	return nil
}
