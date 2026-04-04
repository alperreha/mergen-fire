package main

import (
	"context"
	"fmt"
	"os"

	"github.com/alperreha/mergen-fire/internal/daemon"
)

func main() {
	if err := daemon.RunFromEnv(context.Background()); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
