package main

import "os/exec"

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
