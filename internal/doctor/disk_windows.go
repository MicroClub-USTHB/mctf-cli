//go:build windows

package doctor

// checkDiskSpace is skipped on windows until x/sys/windows integration.
func checkDiskSpace(dir string) Check {
	return Check{
		ID: "D041", Name: "free disk space",
		Status: StatusSkip, Detail: "not implemented on windows",
	}
}
