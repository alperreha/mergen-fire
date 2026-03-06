package postdelete

import (
	"context"

	lifecyclehooks "github.com/alperreha/mergen-fire/cmd/mergen-lifecycle/hooks"
)

// HandleTemplate validates request payload for post-delete extensions.
func HandleTemplate(_ context.Context, req lifecyclehooks.Request) error {
	if err := req.ValidateBase(); err != nil {
		return err
	}
	return req.ValidateVMConfigIfPresent()
}
