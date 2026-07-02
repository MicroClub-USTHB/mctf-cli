//go:build !windows

package doctor

import (
	"fmt"
	"syscall"
)

// checkDiskSpace verifies the host has enough free space to deploy challenges.
func checkDiskSpace(dir string) Check {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return Check{
			ID: "D041", Name: "free disk space",
			Status: StatusSkip, Detail: fmt.Sprintf("statfs failed: %v", err),
		}
	}
	// Available bytes for unprivileged users = Bavail * Bsize
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	freeGB := freeBytes / (1024 * 1024 * 1024)

	// Threshold: 10 GB
	if freeGB < 10 {
		return Check{
			ID: "D041", Name: "free disk space",
			Status: StatusWarn, Detail: fmt.Sprintf("only %d GB free", freeGB),
			Suggest: "free up space before massive deployments to avoid docker crashes",
		}
	}
	return Check{
		ID: "D041", Name: "free disk space",
		Status: StatusOK, Detail: fmt.Sprintf("%d GB free", freeGB),
	}
}
