package network

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"strings"

	"github.com/alperreha/mergen-fire/internal/model"
)

type Allocator struct {
	portStart int
	portEnd   int
	guestCIDR string
	logger    *slog.Logger
}

func NewAllocator(portStart, portEnd int, guestCIDR string) *Allocator {
	return &Allocator{
		portStart: portStart,
		portEnd:   portEnd,
		guestCIDR: guestCIDR,
		logger:    slog.Default(),
	}
}

func (a *Allocator) WithLogger(logger *slog.Logger) *Allocator {
	if logger != nil {
		a.logger = logger
	}
	return a
}

func (a *Allocator) Allocate(existing []model.VMMetadata, requests []model.PortBindingRequest) (string, []model.PortBinding, error) {
	a.logger.Debug("allocation started", "existingVMs", len(existing), "requestedPorts", len(requests), "guestCIDR", a.guestCIDR)
	guestIP, err := a.allocateGuestIP(existing)
	if err != nil {
		return "", nil, err
	}

	portBindings, err := a.allocatePorts(existing, requests)
	if err != nil {
		return "", nil, err
	}

	a.logger.Debug("allocation completed", "guestIP", guestIP, "allocatedPorts", len(portBindings))
	return guestIP, portBindings, nil
}

func (a *Allocator) allocatePorts(existing []model.VMMetadata, requests []model.PortBindingRequest) ([]model.PortBinding, error) {
	used := map[int]struct{}{}
	for _, vm := range existing {
		for _, port := range vm.Ports {
			used[port.Host] = struct{}{}
		}
	}

	bindings := make([]model.PortBinding, 0, len(requests))
	reserved := map[int]struct{}{}

	for _, req := range requests {
		if req.Guest <= 0 || req.Guest > 65535 {
			return nil, fmt.Errorf("guest port is invalid: %d", req.Guest)
		}
		if req.Host < 0 || req.Host > 65535 {
			return nil, fmt.Errorf("host port is invalid: %d", req.Host)
		}

		protocol := strings.TrimSpace(strings.ToLower(req.Protocol))
		if protocol == "" {
			protocol = "tcp"
		}
		if protocol != "tcp" && protocol != "udp" {
			return nil, fmt.Errorf("unsupported protocol: %s", protocol)
		}

		hostPort := req.Host
		if hostPort == 0 {
			hostPort = a.nextFreePort(used, reserved)
			if hostPort == 0 {
				return nil, errors.New("no available host port in configured range")
			}
		}

		if _, ok := used[hostPort]; ok {
			return nil, fmt.Errorf("host port already allocated: %d", hostPort)
		}
		if _, ok := reserved[hostPort]; ok {
			return nil, fmt.Errorf("duplicate host port requested in payload: %d", hostPort)
		}

		reserved[hostPort] = struct{}{}
		bindings = append(bindings, model.PortBinding{
			Guest:    req.Guest,
			Host:     hostPort,
			Protocol: protocol,
		})
		a.logger.Debug("allocated host port", "guestPort", req.Guest, "hostPort", hostPort, "protocol", protocol)
	}

	sort.Slice(bindings, func(i, j int) bool {
		return bindings[i].Host < bindings[j].Host
	})

	return bindings, nil
}

func (a *Allocator) nextFreePort(used, reserved map[int]struct{}) int {
	for port := a.portStart; port <= a.portEnd; port++ {
		if _, exists := used[port]; exists {
			continue
		}
		if _, exists := reserved[port]; exists {
			continue
		}
		return port
	}
	return 0
}

func (a *Allocator) allocateGuestIP(existing []model.VMMetadata) (string, error) {
	prefix, err := netip.ParsePrefix(a.guestCIDR)
	if err != nil {
		return "", fmt.Errorf("invalid guest cidr: %w", err)
	}
	if !prefix.Addr().Is4() {
		return "", errors.New("only IPv4 guest CIDR is supported")
	}

	used := map[string]struct{}{}
	for _, vm := range existing {
		if vm.GuestIP != "" {
			used[vm.GuestIP] = struct{}{}
		}
	}

	networkU32 := ipv4ToU32(prefix.Masked().Addr())
	hostBits := 32 - prefix.Bits()
	if hostBits <= 1 {
		return "", errors.New("guest cidr has no usable host range")
	}

	maxHost := uint32((1 << hostBits) - 1)
	for host := uint32(2); host < maxHost; host++ {
		candidate := u32ToIPv4(networkU32 + host)
		if !prefix.Contains(candidate) {
			continue
		}
		if _, ok := used[candidate.String()]; ok {
			continue
		}
		return candidate.String(), nil
	}

	return "", errors.New("no available guest IP address in CIDR")
}

func TapName(id string) string {
	shortID := id
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return "tap-" + shortID
}

func NetNSName(id string) string {
	shortID := id
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return "mergen-" + shortID
}

func GuestMAC(id string) string {
	hexOnly := strings.ReplaceAll(id, "-", "")
	if len(hexOnly) < 6 {
		return "02:FC:00:00:00:01"
	}
	a := hexOnly[0:2]
	b := hexOnly[2:4]
	c := hexOnly[4:6]
	return fmt.Sprintf("02:FC:%s:%s:%s:01", strings.ToUpper(a), strings.ToUpper(b), strings.ToUpper(c))
}

func ipv4ToU32(addr netip.Addr) uint32 {
	bytes := addr.As4()
	return uint32(bytes[0])<<24 | uint32(bytes[1])<<16 | uint32(bytes[2])<<8 | uint32(bytes[3])
}

func u32ToIPv4(value uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{
		byte(value >> 24),
		byte(value >> 16),
		byte(value >> 8),
		byte(value),
	})
}
