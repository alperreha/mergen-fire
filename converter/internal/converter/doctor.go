package converter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type DoctorOptions struct {
	BaseDir        string
	RequireEnvDisk bool
}

type DoctorCheck struct {
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	Status   string `json:"status"`
	Required bool   `json:"required"`
	Message  string `json:"message"`
}

type DoctorReport struct {
	BaseDir         string        `json:"baseDir"`
	ResolvedBaseDir string        `json:"resolvedBaseDir,omitempty"`
	Checks          []DoctorCheck `json:"checks"`
	Passed          bool          `json:"passed"`
	FailedRequired  int           `json:"failedRequired"`
	Warnings        int           `json:"warnings"`
}

func RunDoctor(opts DoctorOptions) DoctorReport {
	baseDir := strings.TrimSpace(opts.BaseDir)
	if baseDir == "" {
		baseDir = defaultBaseAssetsDir
	}

	report := DoctorReport{BaseDir: baseDir}
	appendCheck := func(check DoctorCheck) {
		report.Checks = append(report.Checks, check)
		if check.Status == "fail" && check.Required {
			report.FailedRequired++
		}
		if check.Status == "warn" {
			report.Warnings++
		}
	}

	if filepath.Base(baseDir) == "current" {
		symlinkCheck, resolvedDir := checkCurrentSymlink("base-current-symlink", baseDir, true)
		appendCheck(symlinkCheck)
		if symlinkCheck.Status == "pass" && resolvedDir != "" {
			report.ResolvedBaseDir = resolvedDir
		}
	}

	appendCheck(checkDirExists("base-dir", baseDir, true))
	appendCheck(checkRegularFile("kernel", filepath.Join(baseDir, "vmlinux"), true))
	appendCheck(checkExt4Image("golden-rootfs-ext4", filepath.Join(baseDir, "golden-rootfs.ext4"), true))
	appendCheck(checkExt4Image("agent-rootfs-ext4", filepath.Join(baseDir, "agent-rootfs.ext4"), true))

	envRequired := opts.RequireEnvDisk
	appendCheck(checkExt4Image("env-rootfs-ext4", filepath.Join(baseDir, "env-rootfs.ext4"), envRequired))

	appendCheck(checkExecutable("sbin-init", filepath.Join(baseDir, "bin", "sbin-init"), true))
	appendCheck(checkExecutable("mergen-agent", filepath.Join(baseDir, "bin", "mergen-agent"), true))
	appendCheck(checkExecutable("mergen-supervisor", filepath.Join(baseDir, "bin", "mergen-supervisor"), false))
	appendCheck(checkExecutable("mergen-telemetry", filepath.Join(baseDir, "bin", "mergen-telemetry"), false))

	report.Passed = report.FailedRequired == 0
	return report
}

func checkCurrentSymlink(name, path string, required bool) (DoctorCheck, string) {
	info, err := os.Lstat(path)
	if err != nil {
		return buildMissingCheck(name, path, required, err), ""
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return DoctorCheck{
			Name:     name,
			Path:     path,
			Status:   "fail",
			Required: required,
			Message:  "expected a symlink (current -> <version>)",
		}, ""
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return DoctorCheck{
			Name:     name,
			Path:     path,
			Status:   "fail",
			Required: required,
			Message:  fmt.Sprintf("resolve symlink failed: %v", err),
		}, ""
	}

	targetInfo, err := os.Stat(resolved)
	if err != nil {
		return DoctorCheck{
			Name:     name,
			Path:     path,
			Status:   "fail",
			Required: required,
			Message:  fmt.Sprintf("symlink target missing: %v", err),
		}, resolved
	}
	if !targetInfo.IsDir() {
		return DoctorCheck{
			Name:     name,
			Path:     path,
			Status:   "fail",
			Required: required,
			Message:  "symlink target is not a directory",
		}, resolved
	}

	return DoctorCheck{
		Name:     name,
		Path:     path,
		Status:   "pass",
		Required: required,
		Message:  fmt.Sprintf("symlink points to %s", resolved),
	}, resolved
}

func checkDirExists(name, path string, required bool) DoctorCheck {
	info, err := os.Stat(path)
	if err != nil {
		return buildMissingCheck(name, path, required, err)
	}
	if !info.IsDir() {
		return DoctorCheck{Name: name, Path: path, Status: "fail", Required: required, Message: "path exists but is not a directory"}
	}
	return DoctorCheck{Name: name, Path: path, Status: "pass", Required: required, Message: "directory exists"}
}

func checkRegularFile(name, path string, required bool) DoctorCheck {
	info, err := os.Stat(path)
	if err != nil {
		return buildMissingCheck(name, path, required, err)
	}
	if !info.Mode().IsRegular() {
		return DoctorCheck{Name: name, Path: path, Status: "fail", Required: required, Message: "path exists but is not a regular file"}
	}
	if info.Mode().Perm()&0o222 != 0 {
		return DoctorCheck{Name: name, Path: path, Status: "warn", Required: required, Message: "file is writable; consider read-only permissions for base assets"}
	}
	return DoctorCheck{Name: name, Path: path, Status: "pass", Required: required, Message: "file exists"}
}

func checkExecutable(name, path string, required bool) DoctorCheck {
	info, err := os.Stat(path)
	if err != nil {
		return buildMissingCheck(name, path, required, err)
	}
	if !info.Mode().IsRegular() {
		return DoctorCheck{Name: name, Path: path, Status: "fail", Required: required, Message: "path exists but is not a regular file"}
	}
	if info.Mode()&0o111 == 0 {
		return DoctorCheck{Name: name, Path: path, Status: "fail", Required: required, Message: "file exists but is not executable"}
	}
	if info.Mode().Perm()&0o222 != 0 {
		return DoctorCheck{Name: name, Path: path, Status: "warn", Required: required, Message: "binary is writable; consider read-only permissions for base assets"}
	}
	return DoctorCheck{Name: name, Path: path, Status: "pass", Required: required, Message: "executable exists"}
}

func checkExt4Image(name, path string, required bool) DoctorCheck {
	info, err := os.Stat(path)
	if err != nil {
		return buildMissingCheck(name, path, required, err)
	}
	if !info.Mode().IsRegular() {
		return DoctorCheck{Name: name, Path: path, Status: "fail", Required: required, Message: "path exists but is not a regular file"}
	}

	ok, sigErr := hasExt4Signature(path)
	if sigErr != nil {
		return DoctorCheck{Name: name, Path: path, Status: "fail", Required: required, Message: fmt.Sprintf("read ext4 signature failed: %v", sigErr)}
	}
	if !ok {
		return DoctorCheck{Name: name, Path: path, Status: "fail", Required: required, Message: "ext4 signature not detected"}
	}
	if info.Mode().Perm()&0o222 != 0 {
		return DoctorCheck{Name: name, Path: path, Status: "warn", Required: required, Message: "ext4 image is writable; consider read-only permissions for base assets"}
	}
	return DoctorCheck{Name: name, Path: path, Status: "pass", Required: required, Message: "ext4 image looks valid"}
}

func buildMissingCheck(name, path string, required bool, err error) DoctorCheck {
	status := "warn"
	if required {
		status = "fail"
	}
	if os.IsNotExist(err) {
		return DoctorCheck{Name: name, Path: path, Status: status, Required: required, Message: "missing"}
	}
	return DoctorCheck{Name: name, Path: path, Status: status, Required: required, Message: err.Error()}
}

func hasExt4Signature(path string) (bool, error) {
	// ext superblock magic is 0xEF53 at offset 1024 + 56.
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	buf := make([]byte, 2)
	if _, err := f.ReadAt(buf, 1080); err != nil {
		return false, err
	}
	return buf[0] == 0x53 && buf[1] == 0xEF, nil
}
