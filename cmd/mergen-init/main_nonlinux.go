//go:build !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	_, _ = fmt.Fprintln(os.Stderr, "mergen-init is only supported on linux")
	os.Exit(1)
}
