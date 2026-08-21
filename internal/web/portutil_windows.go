//go:build windows

package web

import (
	"os/exec"
	"strconv"
)

// killProcess 使用 taskkill 强制终止进程（Windows）
func killProcess(pid int) error {
	return exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/F").Run()
}

// hasAdminPrivilege 直接尝试 taskkill，失败视为无权限（Windows）
func hasAdminPrivilege() bool {
	return true
}
