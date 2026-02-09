//go:build !firecracker_sdk

package firecracker

import "time"

func NewConfigurator(timeout time.Duration) Configurator {
	return NewRawConfigurator(timeout)
}
