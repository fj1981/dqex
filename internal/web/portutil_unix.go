//go:build !windows

package web

import (
	"os"
	"syscall"
)

// killProcess 发送 SIGTERM 终止进程（Unix）
func killProcess(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}

// hasAdminPrivilege root 权限检测（Unix）
func hasAdminPrivilege() bool {
	return os.Geteuid() == 0
}
