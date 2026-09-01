//go:build !unix

package extsuite

import "os/exec"

func configureProcess(cmd *exec.Cmd) {}
