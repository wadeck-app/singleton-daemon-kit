//go:build !windows

package launcher

import "os/exec"

func setCmdFlags(_ *exec.Cmd) {}
