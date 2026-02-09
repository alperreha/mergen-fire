//go:build firecracker_sdk

package firecracker

import (
	"context"
	"errors"

	_ "github.com/firecracker-microvm/firecracker-go-sdk"

	"github.com/alperreha/mergen-fire/internal/model"
)

type SDKConfigurator struct{}

func NewSDKConfigurator() Configurator {
	return &SDKConfigurator{}
}

func (s *SDKConfigurator) ConfigureAndStart(_ context.Context, _ string, _ model.VMConfig) error {
	return errors.New("firecracker-go-sdk path is placeholder in this build")
}
