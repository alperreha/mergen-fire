//go:build linux

package forwarder

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"golang.org/x/sys/unix"
)

type NetNSDialer struct {
	timeout time.Duration
}

func NewNetNSDialer(timeout time.Duration) NetNSDialer {
	return NetNSDialer{timeout: timeout}
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

	targetPath := filepath.Join("/var/run/netns", netns)
	target, err := os.Open(targetPath)
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

func setns(fd uintptr, nstype int) error {
	return unix.Setns(int(fd), nstype)
}
