//go:build firecracker_sdk

package firecracker

import "time"

func NewConfigurator(_ time.Duration) Configurator {
	return NewSDKConfigurator()
}
