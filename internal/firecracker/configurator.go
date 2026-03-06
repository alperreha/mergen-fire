package firecracker

import (
	"context"

	"github.com/alperreha/mergen-fire/internal/model"
)

type ConfigureOptions struct {
	EnableEntropyDevice bool
}

type Configurator interface {
	ConfigureAndStart(ctx context.Context, socketPath string, cfg model.VMConfig, opts ConfigureOptions) error
}
