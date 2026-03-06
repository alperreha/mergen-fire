package poststop

import (
	"context"
	"strings"

	lifecyclehooks "github.com/alperreha/mergen-fire/cmd/mergen-lifecycle/hooks"
)

func HandleDeleteNetwork(ctx context.Context, req lifecyclehooks.Request) error {
	netnsName := strings.TrimSpace(lifecyclehooks.EnvOrDefault("MGN_NETNS", lifecyclehooks.EnvOrDefault("FC_NETNS", "mergen-"+lifecyclehooks.ShortID(req.VMID))))
	exists, err := lifecyclehooks.NetNSExists(ctx, netnsName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	if err := lifecyclehooks.DeleteNamedNetNS(ctx, netnsName); err != nil {
		if req.Logger != nil {
			req.Logger.Warn("network namespace cleanup failed", "vmID", req.VMID, "netns", netnsName, "error", err)
		}
		return nil
	}

	if req.Logger != nil {
		req.Logger.Info("network namespace deleted", "vmID", req.VMID, "netns", netnsName)
	}
	return nil
}
