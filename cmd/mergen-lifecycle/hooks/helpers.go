package hooks

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"

	"github.com/alperreha/mergen-fire/internal/model"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

func EnvOrDefault(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func ShortID(vmID string) string {
	trimmed := strings.TrimSpace(vmID)
	if len(trimmed) >= 8 {
		return trimmed[:8]
	}
	return trimmed
}

func NetNSExists(ctx context.Context, name string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false, errors.New("netns name is required")
	}

	nsHandle, err := netns.GetFromName(trimmed)
	if err != nil {
		if isNotExistError(err) {
			return false, nil
		}
		return false, err
	}
	defer nsHandle.Close()

	return true, nil
}

func EnsureNamedNetNS(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New("netns name is required")
	}

	exists, err := NetNSExists(ctx, trimmed)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	return runOnLockedThread(ctx, func() error {
		currentNS, err := netns.Get()
		if err != nil {
			return err
		}
		defer currentNS.Close()

		createdNS, err := netns.NewNamed(trimmed)
		if err != nil {
			if errors.Is(err, syscall.EEXIST) {
				return nil
			}
			return err
		}
		defer createdNS.Close()

		if err := netns.Set(currentNS); err != nil {
			return fmt.Errorf("restore host netns after create: %w", err)
		}
		return nil
	})
}

func DeleteNamedNetNS(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New("netns name is required")
	}

	if err := netns.DeleteNamed(trimmed); err != nil {
		if isNotExistError(err) {
			return nil
		}
		return err
	}
	return nil
}

func WithNamedNetNS(ctx context.Context, name string, fn func(handle *netlink.Handle) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New("netns name is required")
	}
	if fn == nil {
		return errors.New("netns callback is required")
	}

	return runOnLockedThread(ctx, func() error {
		currentNS, err := netns.Get()
		if err != nil {
			return err
		}
		defer currentNS.Close()

		targetNS, err := netns.GetFromName(trimmed)
		if err != nil {
			return err
		}
		defer targetNS.Close()

		if err := netns.Set(targetNS); err != nil {
			return fmt.Errorf("enter netns %s: %w", trimmed, err)
		}

		handle, err := netlink.NewHandle()
		if err != nil {
			restoreErr := netns.Set(currentNS)
			return errors.Join(err, restoreErr)
		}
		defer handle.Delete()

		runErr := fn(handle)
		restoreErr := netns.Set(currentNS)
		if restoreErr != nil {
			return errors.Join(runErr, fmt.Errorf("restore host netns: %w", restoreErr))
		}
		return runErr
	})
}

func LinkExistsInNS(ctx context.Context, netnsName, link string) (bool, error) {
	var exists bool
	err := WithNamedNetNS(ctx, netnsName, func(handle *netlink.Handle) error {
		_, err := handle.LinkByName(link)
		if err != nil {
			if IsLinkNotFound(err) {
				exists = false
				return nil
			}
			return err
		}
		exists = true
		return nil
	})
	return exists, err
}

func AddressExistsInNS(ctx context.Context, netnsName, link, cidr string) (bool, error) {
	targetAddr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return false, err
	}

	var exists bool
	err = WithNamedNetNS(ctx, netnsName, func(handle *netlink.Handle) error {
		linkObj, err := handle.LinkByName(link)
		if err != nil {
			return err
		}

		addrs, err := handle.AddrList(linkObj, 0)
		if err != nil {
			return err
		}

		for _, current := range addrs {
			if current.Equal(*targetAddr) {
				exists = true
				return nil
			}
		}
		return nil
	})
	return exists, err
}

func IsLinkNotFound(err error) bool {
	if err == nil {
		return false
	}
	var linkNotFound netlink.LinkNotFoundError
	return errors.As(err, &linkNotFound) || isNotExistError(err)
}

func DeriveHostIPFromGuestIP(guestIP string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(guestIP))
	if err != nil || !addr.Is4() {
		return ""
	}
	octets := addr.As4()
	return fmt.Sprintf("%d.%d.%d.1", octets[0], octets[1], octets[2])
}

func GuestIPFromVMConfig(cfg model.VMConfig) string {
	if cfg.BootSource == nil {
		return ""
	}
	for _, arg := range strings.Fields(cfg.BootSource.BootArgs) {
		if strings.HasPrefix(arg, "ip=") {
			raw := strings.TrimPrefix(arg, "ip=")
			parts := strings.Split(raw, ":")
			if len(parts) > 0 {
				return strings.TrimSpace(parts[0])
			}
		}
	}
	return ""
}

func runOnLockedThread(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer func() {
			if r := recover(); r != nil {
				errCh <- fmt.Errorf("panic in locked thread: %v", r)
			}
		}()

		errCh <- fn()
	}()

	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-doneCh:
		return <-errCh
	}
}

func isNotExistError(err error) bool {
	if err == nil {
		return false
	}
	return os.IsNotExist(err) || errors.Is(err, syscall.ENOENT)
}
