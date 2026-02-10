package forwarder

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alperreha/mergen-fire/internal/model"
)

var ErrVMNotFound = errors.New("vm not found for requested host")

type Resolver struct {
	configRoot   string
	domainPrefix string
	domainSuffix string
	domainTail   string
	cacheTTL     time.Duration
	logger       *slog.Logger

	mu         sync.RWMutex
	cacheUntil time.Time
	cache      map[string]model.VMMetadata
	ordered    []model.VMMetadata
}

func NewResolver(configRoot, domainPrefix, domainSuffix string, cacheTTL time.Duration, logger *slog.Logger) *Resolver {
	if logger == nil {
		logger = slog.Default()
	}
	domainPrefix = normalizeDomainPart(domainPrefix)
	domainSuffix = normalizeDomainPart(domainSuffix)
	if domainSuffix == "" {
		domainSuffix = "localhost"
	}
	if cacheTTL <= 0 {
		cacheTTL = 5 * time.Second
	}

	tail := "." + domainSuffix
	if domainPrefix != "" {
		tail = "." + domainPrefix + tail
	}

	return &Resolver{
		configRoot:   configRoot,
		domainPrefix: domainPrefix,
		domainSuffix: domainSuffix,
		domainTail:   tail,
		cacheTTL:     cacheTTL,
		logger:       logger,
		cache:        map[string]model.VMMetadata{},
		ordered:      nil,
	}
}

func (r *Resolver) Resolve(serverName string) (model.VMMetadata, error) {
	label, err := r.labelFromServerName(serverName)
	if err != nil {
		return model.VMMetadata{}, err
	}

	if err := r.refreshCacheIfNeeded(); err != nil {
		return model.VMMetadata{}, err
	}

	r.mu.RLock()
	meta, ok := r.cache[label]
	r.mu.RUnlock()
	if !ok {
		return model.VMMetadata{}, fmt.Errorf("%w: %s", ErrVMNotFound, serverName)
	}
	return meta, nil
}

func (r *Resolver) ResolveFirst() (model.VMMetadata, error) {
	if err := r.refreshCacheIfNeeded(); err != nil {
		return model.VMMetadata{}, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.ordered) == 0 {
		return model.VMMetadata{}, fmt.Errorf("%w: no vm metadata found", ErrVMNotFound)
	}
	return r.ordered[0], nil
}

func (r *Resolver) labelFromServerName(serverName string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(serverName))
	name = strings.TrimSuffix(name, ".")
	if name == "" {
		return "", errors.New("tls server name is empty")
	}
	if !strings.HasSuffix(name, r.domainTail) {
		return "", fmt.Errorf("server name must end with %s", r.domainTail)
	}
	label := strings.TrimSuffix(name, r.domainTail)
	if label == "" || strings.Contains(label, ".") {
		return "", fmt.Errorf("invalid server name label in %s", serverName)
	}
	return label, nil
}

func (r *Resolver) refreshCacheIfNeeded() error {
	r.mu.RLock()
	cacheValid := time.Now().Before(r.cacheUntil) && len(r.cache) > 0
	r.mu.RUnlock()
	if cacheValid {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Now().Before(r.cacheUntil) && len(r.cache) > 0 {
		return nil
	}

	metas, err := r.readAllMetas()
	if err != nil {
		return err
	}

	sort.SliceStable(metas, func(i, j int) bool {
		left := metas[i].CreatedAt
		right := metas[j].CreatedAt
		if left.IsZero() && right.IsZero() {
			return metas[i].ID < metas[j].ID
		}
		if left.IsZero() {
			return false
		}
		if right.IsZero() {
			return true
		}
		if left.Equal(right) {
			return metas[i].ID < metas[j].ID
		}
		return left.Before(right)
	})

	next := map[string]model.VMMetadata{}
	for _, meta := range metas {
		for _, alias := range aliasesForMeta(meta) {
			if _, exists := next[alias]; exists {
				r.logger.Warn("duplicate alias while building resolver cache", "alias", alias, "vmID", meta.ID)
				continue
			}
			next[alias] = meta
		}
	}

	r.cache = next
	r.ordered = append([]model.VMMetadata(nil), metas...)
	r.cacheUntil = time.Now().Add(r.cacheTTL)
	r.logger.Debug("forwarder resolver cache refreshed", "entries", len(next), "orderedVMs", len(r.ordered), "ttl", r.cacheTTL.String())
	return nil
}

func (r *Resolver) readAllMetas() ([]model.VMMetadata, error) {
	entries, err := os.ReadDir(r.configRoot)
	if err != nil {
		return nil, err
	}

	metas := make([]model.VMMetadata, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaPath := filepath.Join(r.configRoot, entry.Name(), "meta.json")
		content, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var meta model.VMMetadata
		if err := json.Unmarshal(content, &meta); err != nil {
			r.logger.Warn("failed to parse vm metadata", "path", metaPath, "error", err)
			continue
		}
		metas = append(metas, meta)
	}
	return metas, nil
}

func aliasesForMeta(meta model.VMMetadata) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}

	add(meta.ID)
	if len(meta.ID) >= 8 {
		add(meta.ID[:8])
	}

	for _, key := range []string{"host", "hostname", "app", "name"} {
		if meta.Tags != nil {
			add(meta.Tags[key])
		}
		if meta.Metadata != nil {
			value, ok := meta.Metadata[key]
			if !ok {
				continue
			}
			if str, isString := value.(string); isString {
				add(str)
			}
		}
	}

	return out
}
