package main

import (
	"os"
	"os/exec"
)

// noEcho apaga el eco del terminal y devuelve la función que lo restaura.
func noEcho() func() {
	cmd := exec.Command("stty", "-echo")
	cmd.Stdin = os.Stdin
	if cmd.Run() != nil {
		return func() {}
	}
	return func() {
		c := exec.Command("stty", "echo")
		c.Stdin = os.Stdin
		_ = c.Run()
	}
}
