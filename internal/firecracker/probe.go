package firecracker

import (
	"errors"
	"os"
)

func SocketPresent(socketPath string) (bool, error) {
	info, err := os.Stat(socketPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	return info.Mode()&os.ModeSocket != 0, nil
}
