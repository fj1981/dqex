//go:build !windows

package cli

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// findDqexProcessesUnix pgrep -x dqex（精确匹配进程名）
func findDqexProcessesUnix() []procInfo {
	out, err := exec.Command("pgrep", "-x", "dqex").Output()
	if err != nil {
		return nil
	}
	var procs []procInfo
	for _, line := range strings.Split(string(out), "\n") {
		pidStr := strings.TrimSpace(line)
		if pidStr == "" {
			continue
		}
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		name := getProcessNameUnix(pid)
		procs = append(procs, procInfo{PID: pid, Name: name})
	}
	return procs
}

func getProcessNameUnix(pid int) string {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func findDqexProcessesWindows() []procInfo {
	return nil // Unix 不调用
}

// killProc 发送 SIGTERM（Unix）
func killProc(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}

// killProcForce 发送 SIGKILL（Unix）
func killProcForce(pid int) error {
	return syscall.Kill(pid, syscall.SIGKILL)
}

// processExists 检查进程是否存在（Unix：发送信号 0）
func processExists(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
