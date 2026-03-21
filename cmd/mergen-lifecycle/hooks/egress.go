package hooks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	egressSubnetPrefix = 30
	egressBaseA        = 100
	egressBaseB        = 64
)

type EgressConfig struct {
	RootVethName string
	NSVethName   string
	RootCIDR     string
	NSCIDR       string
	RootIP       string
	NSIP         string
	GuestIP      string
	Uplink       string
}

func EnsureNetNSEgress(ctx context.Context, netnsName, vmID, guestIP, uplinkOverride string) (_ EgressConfig, err error) {
	cfg, err := BuildEgressConfig(vmID, guestIP)
	if err != nil {
		return EgressConfig{}, err
	}

	uplink, err := ResolveHostUplink(strings.TrimSpace(uplinkOverride))
	if err != nil {
		return EgressConfig{}, err
	}
	cfg.Uplink = uplink

	if err := ensureIPv4ForwardingRoot(); err != nil {
		return EgressConfig{}, err
	}
	if err := ensureVethPair(ctx, netnsName, cfg); err != nil {
		return EgressConfig{}, err
	}

	defer func() {
		if err == nil {
			return
		}
		_ = deleteIPTablesEgress(cfg)
		_ = deleteHostGuestRoute(cfg)
		_ = deleteRootVeth(cfg.RootVethName)
	}()

	if err := ensureNamespaceEgress(ctx, netnsName, cfg); err != nil {
		return EgressConfig{}, err
	}
	if err := ensureHostGuestRoute(cfg); err != nil {
		return EgressConfig{}, err
	}
	if err := ensureIPTablesEgress(cfg); err != nil {
		return EgressConfig{}, err
	}

	return cfg, nil
}

func DeleteNetNSEgress(ctx context.Context, netnsName, vmID, guestIP, uplinkOverride string) error {
	cfg, err := BuildEgressConfig(vmID, guestIP)
	if err != nil {
		return err
	}

	if uplink, uplinkErr := ResolveHostUplink(strings.TrimSpace(uplinkOverride)); uplinkErr == nil {
		cfg.Uplink = uplink
	}

	if err := deleteIPTablesEgress(cfg); err != nil {
		return err
	}
	if err := deleteHostGuestRoute(cfg); err != nil {
		return err
	}
	if err := deleteRootVeth(cfg.RootVethName); err != nil {
		return err
	}

	exists, err := NetNSExists(ctx, netnsName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	return WithNamedNetNS(ctx, netnsName, func(handle *netlink.Handle) error {
		link, err := handle.LinkByName(cfg.NSVethName)
		if err != nil {
			if IsLinkNotFound(err) {
				return nil
			}
			return err
		}
		if err := handle.LinkDel(link); err != nil && !IsLinkNotFound(err) && !errors.Is(err, syscall.ENODEV) {
			return err
		}
		return nil
	})
}

func BuildEgressConfig(vmID, guestIP string) (EgressConfig, error) {
	rootCIDR, nsCIDR, rootIP, nsIP, err := DeriveEgressPeerCIDRs(guestIP)
	if err != nil {
		return EgressConfig{}, err
	}

	rootVeth, nsVeth := EgressVethNames(vmID)
	return EgressConfig{
		RootVethName: rootVeth,
		NSVethName:   nsVeth,
		RootCIDR:     rootCIDR,
		NSCIDR:       nsCIDR,
		RootIP:       rootIP,
		NSIP:         nsIP,
		GuestIP:      strings.TrimSpace(guestIP),
	}, nil
}

func EgressVethNames(vmID string) (string, string) {
	shortID := ShortID(vmID)
	return "mgnh" + shortID, "mgnn" + shortID
}

func DeriveEgressPeerCIDRs(guestIP string) (rootCIDR, nsCIDR, rootIP, nsIP string, err error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(guestIP))
	if err != nil {
		return "", "", "", "", fmt.Errorf("parse guest ip %q: %w", guestIP, err)
	}
	if !addr.Is4() {
		return "", "", "", "", fmt.Errorf("guest ip must be ipv4: %s", addr.String())
	}

	octets := addr.As4()
	index := uint32(octets[2])<<8 | uint32(octets[3])
	base := uint32(egressBaseA)<<24 | uint32(egressBaseB)<<16
	subnetBase := base + index*4

	rootAddr := ipv4FromU32(subnetBase + 1)
	nsAddr := ipv4FromU32(subnetBase + 2)
	return rootAddr.String() + "/30", nsAddr.String() + "/30", rootAddr.String(), nsAddr.String(), nil
}

func ResolveHostUplink(override string) (string, error) {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed, nil
	}

	handle, err := netlink.NewHandle()
	if err != nil {
		return "", err
	}
	defer handle.Delete()

	routes, err := handle.RouteList(nil, syscall.AF_INET)
	if err != nil {
		return "", err
	}

	for _, route := range routes {
		if route.Dst != nil || route.LinkIndex == 0 {
			continue
		}
		link, err := handle.LinkByIndex(route.LinkIndex)
		if err != nil {
			continue
		}
		name := strings.TrimSpace(link.Attrs().Name)
		if name != "" && name != "lo" {
			return name, nil
		}
	}

	return "", errors.New("unable to resolve host uplink interface from default route")
}

func ensureVethPair(ctx context.Context, netnsName string, cfg EgressConfig) error {
	rootHandle, err := netlink.NewHandle()
	if err != nil {
		return err
	}
	defer rootHandle.Delete()

	rootLink, err := rootHandle.LinkByName(cfg.RootVethName)
	if err != nil && !IsLinkNotFound(err) {
		return err
	}
	rootExists := err == nil

	nsExists, err := LinkExistsInNS(ctx, netnsName, cfg.NSVethName)
	if err != nil {
		return err
	}

	if rootExists != nsExists {
		if rootExists {
			if err := rootHandle.LinkDel(rootLink); err != nil && !IsLinkNotFound(err) && !errors.Is(err, syscall.ENODEV) {
				return err
			}
			rootExists = false
		}
		if nsExists {
			if err := WithNamedNetNS(ctx, netnsName, func(handle *netlink.Handle) error {
				link, err := handle.LinkByName(cfg.NSVethName)
				if err != nil {
					if IsLinkNotFound(err) {
						return nil
					}
					return err
				}
				if err := handle.LinkDel(link); err != nil && !IsLinkNotFound(err) && !errors.Is(err, syscall.ENODEV) {
					return err
				}
				return nil
			}); err != nil {
				return err
			}
			nsExists = false
		}
	}

	if !rootExists && !nsExists {
		targetNS, err := netns.GetFromName(strings.TrimSpace(netnsName))
		if err != nil {
			return err
		}
		defer targetNS.Close()

		veth := &netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{Name: cfg.RootVethName},
			PeerName:  cfg.NSVethName,
		}
		if err := rootHandle.LinkAdd(veth); err != nil && !errors.Is(err, syscall.EEXIST) {
			return err
		}

		nsPeer, err := rootHandle.LinkByName(cfg.NSVethName)
		if err != nil {
			return err
		}
		if err := rootHandle.LinkSetNsFd(nsPeer, int(targetNS)); err != nil {
			return err
		}
	}

	rootLink, err = rootHandle.LinkByName(cfg.RootVethName)
	if err != nil {
		return err
	}
	if err := ensureLinkAddress(rootHandle, rootLink, cfg.RootCIDR); err != nil {
		return err
	}
	if err := rootHandle.LinkSetUp(rootLink); err != nil {
		return err
	}
	return nil
}

func ensureNamespaceEgress(ctx context.Context, netnsName string, cfg EgressConfig) error {
	if err := WithNamedNetNS(ctx, netnsName, func(handle *netlink.Handle) error {
		if err := ensureIPv4ForwardingCurrentNS(); err != nil {
			return err
		}

		link, err := handle.LinkByName(cfg.NSVethName)
		if err != nil {
			return err
		}
		if err := ensureLinkAddress(handle, link, cfg.NSCIDR); err != nil {
			return err
		}
		if err := handle.LinkSetUp(link); err != nil {
			return err
		}

		route := &netlink.Route{
			LinkIndex: link.Attrs().Index,
			Gw:        net.ParseIP(cfg.RootIP),
		}
		if err := handle.RouteReplace(route); err != nil {
			return fmt.Errorf("replace namespace default route via %s: %w", cfg.RootIP, err)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func ensureHostGuestRoute(cfg EgressConfig) error {
	handle, err := netlink.NewHandle()
	if err != nil {
		return err
	}
	defer handle.Delete()

	link, err := handle.LinkByName(cfg.RootVethName)
	if err != nil {
		return err
	}

	dst := &net.IPNet{IP: net.ParseIP(cfg.GuestIP), Mask: net.CIDRMask(32, 32)}
	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Gw:        net.ParseIP(cfg.NSIP),
		Dst:       dst,
	}
	if err := handle.RouteReplace(route); err != nil {
		return fmt.Errorf("replace host route to guest %s via %s: %w", cfg.GuestIP, cfg.NSIP, err)
	}
	return nil
}

func deleteHostGuestRoute(cfg EgressConfig) error {
	handle, err := netlink.NewHandle()
	if err != nil {
		return err
	}
	defer handle.Delete()

	link, err := handle.LinkByName(cfg.RootVethName)
	if err != nil {
		if IsLinkNotFound(err) {
			return nil
		}
		return err
	}

	dst := &net.IPNet{IP: net.ParseIP(cfg.GuestIP), Mask: net.CIDRMask(32, 32)}
	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Gw:        net.ParseIP(cfg.NSIP),
		Dst:       dst,
	}
	if err := handle.RouteDel(route); err != nil && !errors.Is(err, syscall.ESRCH) && !isNotExistError(err) {
		return err
	}
	return nil
}

func deleteRootVeth(name string) error {
	handle, err := netlink.NewHandle()
	if err != nil {
		return err
	}
	defer handle.Delete()

	link, err := handle.LinkByName(strings.TrimSpace(name))
	if err != nil {
		if IsLinkNotFound(err) {
			return nil
		}
		return err
	}
	if err := handle.LinkDel(link); err != nil && !IsLinkNotFound(err) && !errors.Is(err, syscall.ENODEV) {
		return err
	}
	return nil
}

func ensureLinkAddress(handle *netlink.Handle, link netlink.Link, cidr string) error {
	target, err := netlink.ParseAddr(strings.TrimSpace(cidr))
	if err != nil {
		return err
	}

	addrs, err := handle.AddrList(link, syscall.AF_INET)
	if err != nil {
		return err
	}
	for _, current := range addrs {
		if current.Equal(*target) {
			return nil
		}
	}

	if err := handle.AddrAdd(link, target); err != nil && !errors.Is(err, syscall.EEXIST) {
		return err
	}
	return nil
}

func ensureIPv4ForwardingRoot() error {
	return ensureIPv4ForwardingAt("/proc/sys/net/ipv4/ip_forward")
}

func ensureIPv4ForwardingCurrentNS() error {
	return ensureIPv4ForwardingAt("/proc/sys/net/ipv4/ip_forward")
}

func ensureIPv4ForwardingAt(path string) error {
	body, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(body)) == "1" {
		return nil
	}
	if err := os.WriteFile(path, []byte("1\n"), 0o644); err != nil {
		return fmt.Errorf("enable ipv4 forwarding at %s: %w", path, err)
	}
	return nil
}

func ensureIPTablesEgress(cfg EgressConfig) error {
	if cfg.Uplink == "" {
		return errors.New("uplink interface is empty")
	}
	if err := ensureIPTablesRule("filter", "FORWARD", "-i", cfg.RootVethName, "-o", cfg.Uplink, "-j", "ACCEPT"); err != nil {
		return err
	}
	if err := ensureIPTablesRule("filter", "FORWARD", "-i", cfg.Uplink, "-o", cfg.RootVethName, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"); err != nil {
		return err
	}
	if err := ensureIPTablesRule("nat", "POSTROUTING", "-s", cfg.GuestIP+"/32", "-o", cfg.Uplink, "-j", "MASQUERADE"); err != nil {
		return err
	}
	return nil
}

func deleteIPTablesEgress(cfg EgressConfig) error {
	if cfg.Uplink == "" {
		return nil
	}
	if err := deleteIPTablesRule("nat", "POSTROUTING", "-s", cfg.GuestIP+"/32", "-o", cfg.Uplink, "-j", "MASQUERADE"); err != nil {
		return err
	}
	if err := deleteIPTablesRule("filter", "FORWARD", "-i", cfg.Uplink, "-o", cfg.RootVethName, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"); err != nil {
		return err
	}
	if err := deleteIPTablesRule("filter", "FORWARD", "-i", cfg.RootVethName, "-o", cfg.Uplink, "-j", "ACCEPT"); err != nil {
		return err
	}
	return nil
}

func ensureIPTablesRule(table, chain string, ruleArgs ...string) error {
	if _, err := runIPTables(table, append([]string{"-C", chain}, ruleArgs...)...); err == nil {
		return nil
	}
	_, err := runIPTables(table, append([]string{"-A", chain}, ruleArgs...)...)
	return err
}

func deleteIPTablesRule(table, chain string, ruleArgs ...string) error {
	_, err := runIPTables(table, append([]string{"-D", chain}, ruleArgs...)...)
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return nil
	}
	return err
}

func runIPTables(table string, args ...string) ([]byte, error) {
	iptablesPath, err := exec.LookPath("iptables")
	if err != nil {
		return nil, fmt.Errorf("iptables binary not found: %w", err)
	}

	cmdArgs := make([]string, 0, len(args)+3)
	cmdArgs = append(cmdArgs, "-w")
	if trimmed := strings.TrimSpace(table); trimmed != "" {
		cmdArgs = append(cmdArgs, "-t", trimmed)
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command(iptablesPath, cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("iptables %s failed: %w (%s)", strings.Join(cmdArgs, " "), err, strings.TrimSpace(string(bytes.TrimSpace(output))))
	}
	return output, nil
}

func ipv4FromU32(value uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{
		byte(value >> 24),
		byte(value >> 16),
		byte(value >> 8),
		byte(value),
	})
}
