package images

import (
	"fmt"
	"strings"
)

const (
	defaultBaseVersion = "latest"
	defaultBaseFlavor  = "buildroot"
)

type BaseRef struct {
	Platform string
	Flavor   string
	Version  string
}

func NormalizeBaseRef(platform, flavor, version string) (BaseRef, error) {
	ref := BaseRef{
		Platform: normalizeBasePart(platform),
		Flavor:   normalizeBasePart(flavor),
		Version:  normalizeVersion(version),
	}
	if ref.Platform == "" {
		return BaseRef{}, fmt.Errorf("base platform is required")
	}
	if ref.Flavor == "" {
		ref.Flavor = defaultBaseFlavor
	}
	if ref.Version == "" {
		ref.Version = defaultBaseVersion
	}
	return ref, nil
}

func (r BaseRef) LatestAlias() BaseRef {
	r.Version = defaultBaseVersion
	return r
}

func (r BaseRef) Name() string {
	return r.Platform + "/" + r.Flavor + "/" + r.Version
}

func normalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultBaseVersion
	}
	return normalizeBasePart(value)
}

func normalizeBasePart(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}

	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-_.")
	return out
}
