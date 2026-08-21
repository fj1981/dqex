//go:build windows

package cli

import (
	"os/exec"
	"strconv"
	"strings"
)

func findDqexProcessesUnix() []procInfo {
	return nil // Windows 不调用
}

func getProcessNameUnix(pid int) string {
	return "unknown" // Windows 不调用
}

// findDqexProcessesWindows 通过 tasklist 查找 dqex 进程
func findDqexProcessesWindows() []procInfo {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq dqex.exe", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return nil
	}
	var procs []procInfo
	seen := map[int]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		name := strings.Trim(parts[0], "\"")
		pidStr := strings.TrimSpace(parts[1])
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid == 0 || seen[pid] {
			continue
		}
		seen[pid] = true
		procs = append(procs, procInfo{PID: pid, Name: name})
	}
	return procs
}

// killProc 发送 taskkill（Windows）
func killProc(pid int) error {
	return exec.Command("taskkill", "/PID", strconv.Itoa(pid)).Run()
}

// killProcForce 发送 taskkill /F（Windows）
func killProcForce(pid int) error {
	return exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/F").Run()
}

// processExists 检查进程是否存在（Windows：tasklist）
func processExists(pid int) bool {
	out, _ := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH").Output()
	return strings.Contains(string(out), strconv.Itoa(pid))
}
