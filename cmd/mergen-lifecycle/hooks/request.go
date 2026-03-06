package hooks

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/alperreha/mergen-fire/internal/model"
)

type Paths struct {
	VMDir      string
	RunDir     string
	SocketPath string
	VMJSONPath string
	MetaPath   string
}

type Request struct {
	VMID            string
	Stage           string
	Paths           Paths
	VMConfig        model.VMConfig
	VMConfigPresent bool
	Logger          *slog.Logger
}

func (r Request) ValidateBase() error {
	if strings.TrimSpace(r.VMID) == "" {
		return errors.New("vm id is required")
	}
	if strings.TrimSpace(r.Stage) == "" {
		return errors.New("stage is required")
	}
	return nil
}

func (r Request) ValidateVMConfig() error {
	if !r.VMConfigPresent {
		return errors.New("vm config is required")
	}
	return model.ValidateVMConfig(r.VMConfig)
}

func (r Request) ValidateVMConfigIfPresent() error {
	if !r.VMConfigPresent {
		return nil
	}
	return r.ValidateVMConfig()
}
