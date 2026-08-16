//go:build windows

package launcher

import (
	"fmt"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

func assignJobObject(cmd *exec.Cmd) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("CreateJobObject: %w", err)
	}
	// Intentionally NOT closing `job`. JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE requires
	// the handle to remain open for the lifetime of this process: the OS kills all
	// job members when the last handle to the job is closed. Closing it here would
	// terminate the child immediately.

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	// KILL_ON_JOB_CLOSE: kill node when the launcher exits (handle closes).
	// SILENT_BREAKAWAY_OK: processes spawned BY node (updater helper, tray, new
	// launcher after update) automatically leave the Job Object. Without this,
	// they would be killed along with node when the Job Object closes — causing
	// the update-spawned wdrive.exe to die before it can write a single log line.
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
		0x00000100 // JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		return fmt.Errorf("SetInformationJobObject: %w", err)
	}

	ph, err := windows.OpenProcess(windows.PROCESS_ALL_ACCESS, false, uint32(cmd.Process.Pid))
	if err != nil {
		return fmt.Errorf("OpenProcess: %w", err)
	}
	defer windows.CloseHandle(ph)

	if err := windows.AssignProcessToJobObject(job, ph); err != nil {
		return fmt.Errorf("AssignProcessToJobObject: %w", err)
	}
	return nil
}
