package poststop

import (
	"context"

	lifecyclehooks "github.com/alperreha/mergen-fire/cmd/mergen-lifecycle/hooks"
)

// HandleTemplate validates request payload for post-stop extensions.
func HandleTemplate(_ context.Context, req lifecyclehooks.Request) error {
	if err := req.ValidateBase(); err != nil {
		return err
	}
	return req.ValidateVMConfigIfPresent()
}
