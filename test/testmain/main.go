package main

import (
	"os"
	"os/exec"
	"strings"
)

func main() {
	cmd := exec.Command("docker", strings.Split("logs --follow 29754cb1ab9a", " ")...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.Start()
	cmd.Wait()
}
