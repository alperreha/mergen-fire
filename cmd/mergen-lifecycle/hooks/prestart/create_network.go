package prestart

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"

	lifecyclehooks "github.com/alperreha/mergen-fire/cmd/mergen-lifecycle/hooks"
	"github.com/alperreha/mergen-fire/internal/model"
	"github.com/vishvananda/netlink"
)

const defaultHostPrefix = "24"
const (
	linuxIFFTap      = netlink.TuntapMode(0x0002)
	linuxIFFOneQueue = netlink.TuntapFlag(0x2000)
)

func HandleCreateNetwork(ctx context.Context, req lifecyclehooks.Request) error {
	if err := req.ValidateVMConfig(); err != nil {
		return err
	}
	if len(req.VMConfig.NetworkInterfaces) == 0 {
		if req.Logger != nil {
			req.Logger.Debug("network setup skipped; vm has no network interfaces", "vmID", req.VMID)
		}
		return nil
	}

	netnsName := strings.TrimSpace(lifecyclehooks.EnvOrDefault("MGN_NETNS", lifecyclehooks.EnvOrDefault("FC_NETNS", "mergen-"+lifecyclehooks.ShortID(req.VMID))))
	tapName := strings.TrimSpace(lifecyclehooks.EnvOrDefault("MGN_TAP_NAME", defaultTapName(req)))
	guestIP := strings.TrimSpace(lifecyclehooks.EnvOrDefault("MGN_GUEST_IP", lifecyclehooks.GuestIPFromVMConfig(req.VMConfig)))
	hostPrefix := strings.TrimSpace(lifecyclehooks.EnvOrDefault("MGN_HOST_PREFIX", defaultHostPrefix))
	hostIP := strings.TrimSpace(lifecyclehooks.EnvOrDefault("MGN_HOST_IP", ""))
	egressUplink := strings.TrimSpace(lifecyclehooks.EnvOrDefault("MGN_EGRESS_IFACE", ""))
	if hostIP == "" {
		hostIP = lifecyclehooks.DeriveHostIPFromGuestIP(guestIP)
	}

	if err := lifecyclehooks.EnsureNamedNetNS(ctx, netnsName); err != nil {
		return err
	}

	err := lifecyclehooks.WithNamedNetNS(ctx, netnsName, func(handle *netlink.Handle) error {
		loLink, loErr := handle.LinkByName("lo")
		if loErr == nil {
			if upErr := handle.LinkSetUp(loLink); upErr != nil && req.Logger != nil {
				req.Logger.Warn("failed to bring loopback up", "vmID", req.VMID, "netns", netnsName, "error", upErr)
			}
		} else if !lifecyclehooks.IsLinkNotFound(loErr) {
			return loErr
		}

		tapLink, tapErr := handle.LinkByName(tapName)
		if tapErr != nil {
			if !lifecyclehooks.IsLinkNotFound(tapErr) {
				return tapErr
			}

			tap := &netlink.Tuntap{
				LinkAttrs: netlink.LinkAttrs{Name: tapName},
				Mode:      linuxIFFTap,
				Flags:     linuxIFFOneQueue,
			}
			if err := handle.LinkAdd(tap); err != nil && !errors.Is(err, syscall.EEXIST) {
				return err
			}

			tapLink, tapErr = handle.LinkByName(tapName)
			if tapErr != nil {
				return tapErr
			}
		}

		if err := handle.LinkSetUp(tapLink); err != nil {
			return err
		}

		if hostIP != "" {
			hostCIDR := fmt.Sprintf("%s/%s", hostIP, hostPrefix)
			targetAddr, parseErr := netlink.ParseAddr(hostCIDR)
			if parseErr != nil {
				return parseErr
			}

			addrs, addrErr := handle.AddrList(tapLink, 0)
			if addrErr != nil {
				return addrErr
			}

			addrExists := false
			for _, current := range addrs {
				if current.Equal(*targetAddr) {
					addrExists = true
					break
				}
			}
			if !addrExists {
				if err := handle.AddrAdd(tapLink, targetAddr); err != nil && !errors.Is(err, syscall.EEXIST) {
					if req.Logger != nil {
						req.Logger.Warn("host ip assignment skipped", "vmID", req.VMID, "tap", tapName, "address", hostCIDR, "error", err)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	egressCfg, err := lifecyclehooks.EnsureNetNSEgress(ctx, netnsName, req.VMID, guestIP, egressUplink)
	if err != nil {
		return err
	}

	if req.Logger != nil {
		req.Logger.Info(
			"network setup completed",
			"vmID", req.VMID,
			"netns", netnsName,
			"tap", tapName,
			"egressRootVeth", egressCfg.RootVethName,
			"egressNSVeth", egressCfg.NSVethName,
			"egressUplink", egressCfg.Uplink,
		)
	}
	return nil
}

func defaultTapName(req lifecyclehooks.Request) string {
	if len(req.VMConfig.NetworkInterfaces) > 0 {
		if req.VMConfig.NetworkInterfaces[0] != nil {
			hostDev := strings.TrimSpace(model.StringValue(req.VMConfig.NetworkInterfaces[0].HostDevName))
			if hostDev != "" {
				return hostDev
			}
		}
	}
	return "tap-" + lifecyclehooks.ShortID(req.VMID)
}
