//go:build !linux

package forwarder

import (
	"context"
	"errors"
	"net"
	"time"
)

type NetNSDialer struct {
	timeout time.Duration
}

func NewNetNSDialer(timeout time.Duration) NetNSDialer {
	return NetNSDialer{timeout: timeout}
}

func (d NetNSDialer) DialContext(_ context.Context, _, _ string, _ string) (net.Conn, error) {
	_ = d.timeout
	return nil, errors.New("network namespace dial is only supported on linux")
}
