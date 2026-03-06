package firecracker

import (
	"context"
	"time"

	fcmodels "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	fcops "github.com/firecracker-microvm/firecracker-go-sdk/client/operations"
)

func SendCtrlAltDel(ctx context.Context, socketPath string, timeout time.Duration) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	client := newOperationsClient(socketPath)
	params := fcops.NewCreateSyncActionParamsWithContext(ctx)
	action := fcmodels.InstanceActionInfo{
		ActionType: stringPtr(fcmodels.InstanceActionInfoActionTypeSendCtrlAltDel),
	}
	params.SetInfo(&action)
	_, err := client.CreateSyncAction(params)
	return err
}

func GetInstanceInfo(ctx context.Context, socketPath string, timeout time.Duration) (*fcmodels.InstanceInfo, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	client := newOperationsClient(socketPath)
	params := fcops.NewDescribeInstanceParamsWithContext(ctx)
	response, err := client.DescribeInstance(params)
	if err != nil {
		return nil, err
	}
	return response.GetPayload(), nil
}
