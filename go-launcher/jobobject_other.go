//go:build !windows

package launcher

import "os/exec"

func assignJobObject(_ *exec.Cmd) error {
	return nil
}
