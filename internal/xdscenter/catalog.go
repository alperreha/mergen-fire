package xdscenter

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alperreha/mergen-fire/internal/model"
)

type Catalog struct {
	configRoot string
	domain     string
	netnsRoot  string
	logger     *slog.Logger
}

func NewCatalog(configRoot, domain, netnsRoot string, logger *slog.Logger) *Catalog {
	if logger == nil {
		logger = slog.Default()
	}
	root := strings.TrimSpace(netnsRoot)
	if root == "" {
		root = "/run/netns"
	}
	return &Catalog{
		configRoot: configRoot,
		domain:     domain,
		netnsRoot:  root,
		logger:     logger,
	}
}

func (c *Catalog) ListRoutes() ([]RouteRecord, error) {
	metas, err := c.readAllMetas()
	if err != nil {
		return nil, err
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

	seenHosts := map[string]struct{}{}
	out := make([]RouteRecord, 0, len(metas)*3)
	for _, meta := range metas {
		for _, alias := range aliasesForMeta(meta) {
			record, recErr := routeFromMeta(meta, alias, c.domain, c.netnsRoot)
			if recErr != nil {
				c.logger.Warn("skipping route record", "vmID", meta.ID, "alias", alias, "error", recErr)
				continue
			}
			if _, exists := seenHosts[record.Host]; exists {
				c.logger.Warn("duplicate host alias detected while building routes", "host", record.Host, "vmID", meta.ID)
				continue
			}
			seenHosts[record.Host] = struct{}{}
			out = append(out, record)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Host < out[j].Host
	})
	return out, nil
}

func routeFromMeta(meta model.VMMetadata, label, domain, netnsRoot string) (RouteRecord, error) {
	if strings.TrimSpace(label) == "" {
		return RouteRecord{}, fmt.Errorf("label is empty")
	}
	if meta.HTTPPort <= 0 || meta.HTTPPort > 65535 {
		return RouteRecord{}, fmt.Errorf("invalid httpPort for vm %s: %d", meta.ID, meta.HTTPPort)
	}
	if strings.TrimSpace(meta.GuestIP) == "" {
		return RouteRecord{}, fmt.Errorf("guestIP is empty for vm %s", meta.ID)
	}

	host := label + "." + domain
	targetAddr := net.JoinHostPort(meta.GuestIP, fmt.Sprintf("%d", meta.HTTPPort))

	return RouteRecord{
		Host:             host,
		Label:            label,
		VMID:             meta.ID,
		GuestIP:          meta.GuestIP,
		GuestPort:        meta.HTTPPort,
		NetNS:            meta.NetNS,
		NetNSPath:        filepath.Join(netnsRoot, meta.NetNS),
		TargetAddr:       targetAddr,
		ClusterName:      "vm-" + safeName(meta.ID),
		CreatedAt:        meta.CreatedAt,
		Source:           "meta.json",
		ResolvedAt:       time.Now().UTC(),
		ResolverStrategy: "filesystem-alias-cache",
	}, nil
}

func safeName(value string) string {
	out := strings.ToLower(strings.TrimSpace(value))
	if out == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer(
		".", "-",
		"_", "-",
		"/", "-",
		" ", "-",
	)
	out = replacer.Replace(out)
	return out
}

func labelFromHost(host, domain string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(host))
	name = strings.TrimSuffix(name, ".")
	if name == "" {
		return "", fmt.Errorf("host is empty")
	}
	tail := "." + strings.ToLower(strings.TrimSpace(domain))
	if !strings.HasSuffix(name, tail) {
		return "", fmt.Errorf("host %q does not end with %q", host, tail)
	}
	label := strings.TrimSuffix(name, tail)
	if label == "" || strings.Contains(label, ".") {
		return "", fmt.Errorf("invalid label in host %q", host)
	}
	return label, nil
}

func (c *Catalog) readAllMetas() ([]model.VMMetadata, error) {
	entries, err := os.ReadDir(c.configRoot)
	if err != nil {
		return nil, err
	}

	metas := make([]model.VMMetadata, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaPath := filepath.Join(c.configRoot, entry.Name(), "meta.json")
		content, readErr := os.ReadFile(metaPath)
		if readErr != nil {
			continue
		}
		var meta model.VMMetadata
		if unmarshalErr := json.Unmarshal(content, &meta); unmarshalErr != nil {
			c.logger.Warn("failed to parse vm metadata", "path", metaPath, "error", unmarshalErr)
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
