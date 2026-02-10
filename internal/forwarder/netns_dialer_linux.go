//go:build linux

package forwarder

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type NetNSDialer struct {
	timeout time.Duration
	roots   []string
}

func NewNetNSDialer(timeout time.Duration, netnsRoot string) NetNSDialer {
	root := strings.TrimSpace(netnsRoot)
	if root == "" {
		root = "/run/netns"
	}

	roots := make([]string, 0, 2)
	roots = append(roots, root)
	for _, fallback := range []string{"/run/netns", "/var/run/netns"} {
		if root == fallback {
			continue
		}
		roots = append(roots, fallback)
	}

	return NetNSDialer{
		timeout: timeout,
		roots:   roots,
	}
}

func (d NetNSDialer) DialContext(ctx context.Context, network, address, netns string) (net.Conn, error) {
	if netns == "" {
		return nil, fmt.Errorf("netns is empty")
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origin, err := os.Open("/proc/self/ns/net")
	if err != nil {
		return nil, err
	}
	defer origin.Close()

	targetPath, target, err := d.openTargetNS(netns)
	if err != nil {
		return nil, err
	}
	defer target.Close()

	if err := setns(target.Fd(), unix.CLONE_NEWNET); err != nil {
		return nil, fmt.Errorf("setns target %s failed: %w", targetPath, err)
	}
	defer func() {
		_ = setns(origin.Fd(), unix.CLONE_NEWNET)
	}()

	dialer := &net.Dialer{
		Timeout: d.timeout,
	}
	return dialer.DialContext(ctx, network, address)
}

func (d NetNSDialer) openTargetNS(netns string) (string, *os.File, error) {
	var lastErr error
	for _, root := range d.roots {
		targetPath := filepath.Join(root, netns)
		target, err := os.Open(targetPath)
		if err == nil {
			return targetPath, target, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("netns path resolution failed")
	}
	return "", nil, fmt.Errorf("open netns %q under %v: %w", netns, d.roots, lastErr)
}

func setns(fd uintptr, nstype int) error {
	return unix.Setns(int(fd), nstype)
}
