package firecracker

import (
	"context"

	"github.com/alperreha/mergen-fire/internal/model"
)

type Configurator interface {
	ConfigureAndStart(ctx context.Context, socketPath string, cfg model.VMConfig) error
}
