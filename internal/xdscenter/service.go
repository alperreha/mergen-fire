package xdscenter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/alperreha/mergen-fire/internal/forwarder"
)

type Service struct {
	cfg      Config
	resolver *forwarder.Resolver
	catalog  *Catalog
	consul   *ConsulPublisher
	logger   *slog.Logger
}

func NewService(cfg Config, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	resolver := forwarder.NewResolver(cfg.ConfigRoot, cfg.Domain, cfg.ResolverCacheTTL, logger.With("component", "resolver"))
	catalog := NewCatalog(cfg.ConfigRoot, cfg.Domain, cfg.NetNSRoot, logger.With("component", "catalog"))
	consul := NewConsulPublisher(cfg.ConsulHTTPAddr, cfg.ConsulHTTPToken, cfg.ConsulKVPrefix, cfg.RequestTimeout, logger.With("component", "consul"))
	return &Service{
		cfg:      cfg,
		resolver: resolver,
		catalog:  catalog,
		consul:   consul,
		logger:   logger,
	}
}

func (s *Service) Resolve(host string) (RouteRecord, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return RouteRecord{}, fmt.Errorf("host is empty")
	}

	meta, err := s.resolver.Resolve(host)
	if err != nil {
		return RouteRecord{}, err
	}

	label, err := labelFromHost(host, s.cfg.Domain)
	if err != nil {
		return RouteRecord{}, err
	}

	record, err := routeFromMeta(meta, label, s.cfg.Domain, s.cfg.NetNSRoot)
	if err != nil {
		return RouteRecord{}, err
	}
	record.Source = "resolver"
	record.ResolverStrategy = "sni-label-alias"
	record.ResolvedAt = time.Now().UTC()
	return record, nil
}

func (s *Service) ListRoutes() ([]RouteRecord, error) {
	return s.catalog.ListRoutes()
}

func (s *Service) SyncConsul(ctx context.Context) (ConsulSyncResult, error) {
	if !s.consul.Enabled() {
		return ConsulSyncResult{
			Enabled: false,
			Count:   0,
			Prefix:  s.consul.Prefix(),
		}, nil
	}
	routes, err := s.ListRoutes()
	if err != nil {
		return ConsulSyncResult{}, err
	}
	count, err := s.consul.SyncRoutes(ctx, routes)
	if err != nil {
		return ConsulSyncResult{}, err
	}
	return ConsulSyncResult{
		Enabled: true,
		Count:   count,
		Prefix:  s.consul.Prefix(),
	}, nil
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "ok",
			"service": "mergen-xds-center",
			"domain":  s.cfg.Domain,
			"pid":     os.Getpid(),
		})
	})

	mux.HandleFunc("/v1/routes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}

		routes, err := s.ListRoutes()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, RoutesResponse{
			Domain: s.cfg.Domain,
			Count:  len(routes),
			Routes: routes,
		})
	})

	mux.HandleFunc("/v1/routes/resolve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		host := strings.TrimSpace(r.URL.Query().Get("host"))
		if host == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query param host is required"})
			return
		}
		route, err := s.Resolve(host)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, route)
	})

	mux.HandleFunc("/v1/consul/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RequestTimeout)
		defer cancel()

		result, err := s.SyncConsul(ctx)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	return mux
}

func (s *Service) Run(ctx context.Context) error {
	server := &http.Server{
		Addr:              s.cfg.HTTPAddr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: s.cfg.RequestTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info(
			"mergen-xds-center started",
			"addr", s.cfg.HTTPAddr,
			"domain", s.cfg.Domain,
			"configRoot", s.cfg.ConfigRoot,
			"netnsRoot", s.cfg.NetNSRoot,
			"consulAddr", s.cfg.ConsulHTTPAddr,
			"consulPrefix", s.cfg.ConsulKVPrefix,
		)
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errCh; err != nil {
		return err
	}
	s.logger.Info("mergen-xds-center stopped")
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
}
