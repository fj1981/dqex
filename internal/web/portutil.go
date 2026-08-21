package web

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

// ProcInfo 端口占用进程信息
type ProcInfo struct {
	PID  int
	Name string
}

// ensurePortAvailable 端口预检：占用时交互提示终止占用进程，重试绑定。
// 返回 true 表示端口可用（已释放或原本空闲），false 表示无法使用端口（放弃启动）。
func ensurePortAvailable(host string, port int, lang string) bool {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	if tryListen(addr) == nil {
		return true
	}
	// 监听 0.0.0.0 时额外检查 127.0.0.1（可能有进程仅绑定回环地址）
	if host == "0.0.0.0" || host == "::" {
		if tryListen(net.JoinHostPort("127.0.0.1", strconv.Itoa(port))) == nil {
			return true
		}
	}
	txt := portTextsFor(lang)
	procs := findProcessByPort(host, port)
	// 无法识别占用进程
	if len(procs) == 0 {
		fmt.Fprintf(os.Stderr, txt.portOccupiedUnknown+"\n", port)
		fmt.Fprintln(os.Stderr, txt.queryHint)
		fmt.Fprintln(os.Stderr, queryCmdUnix(port))
		fmt.Fprintln(os.Stderr, queryCmdWin(port))
		fmt.Fprintf(os.Stderr, txt.altPort+"\n", port+1)
		return false
	}
	// 展示占用进程信息
	printPortInfo(os.Stderr, procs, port, txt)
	// 非 TTY 环境不交互
	if !isTTY() {
		fmt.Fprintf(os.Stderr, txt.altPort+"\n", port+1)
		return false
	}
	// 交互提示
	if !promptKill(txt) {
		fmt.Fprintln(os.Stderr, txt.cancelled)
		return false
	}
	// 权限预检（Unix: euid==0；Windows: 直接尝试，失败视为无权限）
	if !hasAdminPrivilege() {
		printNoPermissionHint(os.Stderr, procs, port, txt)
		return false
	}
	// 终止占用进程
	permDenied := false
	for _, p := range procs {
		fmt.Fprintf(os.Stderr, txt.killing+"\n", p.PID)
		if err := killProcess(p.PID); err != nil {
			permDenied = true
		}
	}
	if permDenied {
		printNoPermissionHint(os.Stderr, procs, port, txt)
		return false
	}
	// 等待端口释放并重试
	for i := 0; i < 10; i++ {
		if tryListen(addr) == nil {
			fmt.Fprintf(os.Stderr, txt.freed+"\n", port)
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	fmt.Fprintf(os.Stderr, txt.stillOccupied+"\n", port)
	fmt.Fprintf(os.Stderr, txt.altPort+"\n", port+1)
	return false
}

// printNoPermissionHint 无权限时输出手动操作提示（终止命令 + 查询命令 + 换端口建议）
func printNoPermissionHint(w io.Writer, procs []ProcInfo, port int, txt portTexts) {
	fmt.Fprintln(w, txt.noPermission)
	for _, p := range procs {
		fmt.Fprintf(w, txt.sudoCmd+"\n", p.PID)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, txt.queryHint)
	fmt.Fprintln(w, queryCmdUnix(port))
	fmt.Fprintln(w, queryCmdWin(port))
	fmt.Fprintf(w, txt.altPort+"\n", port+1)
}

// tryListen 尝试绑定端口，成功立即关闭并返回 nil
func tryListen(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	ln.Close()
	return nil
}

// ---- 进程查找（跨平台） ----

func findProcessByPort(host string, port int) []ProcInfo {
	switch runtime.GOOS {
	case "windows":
		return findProcessByPortWindows(port)
	default:
		return findProcessByPortUnix(port)
	}
}

// findProcessByPortUnix lsof 优先，降级 fuser → ss（仅 Linux）
func findProcessByPortUnix(port int) []ProcInfo {
	// lsof（macOS 始终存在，大多数 Linux 发行版默认安装）
	out, err := exec.Command("lsof", "-i", ":"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fn", "-Fp").Output()
	if err == nil {
		return parseLsofOutput(string(out))
	}
	if runtime.GOOS == "linux" {
		// fuser 降级
		if out, err := exec.Command("fuser", strconv.Itoa(port)+"/tcp").CombinedOutput(); err == nil {
			return parseFuserOutput(string(out))
		}
		// ss 降级
		if out, err := exec.Command("ss", "-tlnp", "sport", "=", strconv.Itoa(port)).Output(); err == nil {
			return parseSsOutput(string(out))
		}
	}
	return nil
}

// parseLsofOutput 解析 lsof -Fn -Fp 输出（pPID / nNAME 成对，空行分隔条目）
func parseLsofOutput(out string) []ProcInfo {
	var procs []ProcInfo
	var cur ProcInfo
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "p"):
			if pid, err := strconv.Atoi(line[1:]); err == nil {
				cur.PID = pid
			}
		case strings.HasPrefix(line, "n"):
			cur.Name = line[1:]
		case line == "" && cur.PID > 0:
			procs = append(procs, cur)
			cur = ProcInfo{}
		}
	}
	if cur.PID > 0 {
		procs = append(procs, cur)
	}
	return dedup(procs)
}

// parseFuserOutput fuser 输出格式: "  12345  67890\n"
func parseFuserOutput(out string) []ProcInfo {
	var procs []ProcInfo
	for _, f := range strings.Fields(out) {
		if pid, err := strconv.Atoi(f); err == nil {
			procs = append(procs, ProcInfo{PID: pid, Name: "unknown"})
		}
	}
	return procs
}

// parseSsOutput ss -tlnp 输出含 users:(("name",pid=123,fd=N))
func parseSsOutput(out string) []ProcInfo {
	var procs []ProcInfo
	for _, line := range strings.Split(out, "\n") {
		idx := strings.Index(line, "users:")
		if idx < 0 {
			continue
		}
		users := line[idx:]
		name := "unknown"
		if q1 := strings.Index(users, "\""); q1 >= 0 {
			if q2 := strings.Index(users[q1+1:], "\""); q2 >= 0 {
				name = users[q1+1 : q1+1+q2]
			}
		}
		if p := strings.Index(users, "pid="); p >= 0 {
			rest := users[p+4:]
			end := strings.IndexAny(rest, ",)")
			if end < 0 {
				end = len(rest)
			}
			if pid, err := strconv.Atoi(rest[:end]); err == nil {
				procs = append(procs, ProcInfo{PID: pid, Name: name})
			}
		}
	}
	return dedup(procs)
}

func findProcessByPortWindows(port int) []ProcInfo {
	out, err := exec.Command("cmd", "/C", "netstat -ano | findstr :"+strconv.Itoa(port)+" LISTENING").Output()
	if err != nil {
		return nil
	}
	seen := map[int]bool{}
	var procs []ProcInfo
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		pid, err := strconv.Atoi(fields[len(fields)-1])
		if err != nil || pid == 0 || seen[pid] {
			continue
		}
		seen[pid] = true
		name := getProcessNameWindows(pid)
		procs = append(procs, ProcInfo{PID: pid, Name: name})
	}
	return procs
}

func getProcessNameWindows(pid int) string {
	out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH").Output()
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) >= 1 {
			return strings.Trim(parts[0], "\"")
		}
	}
	return "unknown"
}

// ---- 进程终止 ----

func killProcess(pid int) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/F").Run()
	default:
		return syscall.Kill(pid, syscall.SIGTERM)
	}
}

// ---- 权限检测 ----

func hasAdminPrivilege() bool {
	switch runtime.GOOS {
	case "windows":
		return true // 直接尝试 taskkill，失败视为无权限
	default:
		return os.Geteuid() == 0
	}
}

// ---- 辅助函数 ----

func isTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func promptKill(txt portTexts) bool {
	fmt.Print(txt.promptKill)
	var answer string
	fmt.Scanln(&answer)
	return strings.EqualFold(strings.TrimSpace(answer), "y")
}

func printPortInfo(w io.Writer, procs []ProcInfo, port int, txt portTexts) {
	fmt.Fprintf(w, txt.portOccupied+"\n", port)
	for _, p := range procs {
		fmt.Fprintf(w, txt.procLine+"\n", p.PID, p.Name)
	}
}

func queryCmdUnix(port int) string {
	return fmt.Sprintf("  macOS/Linux:  lsof -i :%d", port)
}

func queryCmdWin(port int) string {
	return fmt.Sprintf("  Windows:      netstat -ano | findstr :%d", port)
}

func dedup(procs []ProcInfo) []ProcInfo {
	seen := map[int]bool{}
	var result []ProcInfo
	for _, p := range procs {
		if !seen[p.PID] {
			seen[p.PID] = true
			result = append(result, p)
		}
	}
	return result
}

// ---- 双语文本 ----

type portTexts struct {
	portOccupied        string // "端口 %d 已被占用:"
	portOccupiedUnknown string // "端口 %d 已被占用，但无法识别占用进程。"
	procLine            string // "  PID %d  %s"
	promptKill          string // "是否终止占用进程? [y/N] "
	killing             string // "正在终止进程 %d ..."
	freed               string // "端口 %d 已释放，正在启动服务..."
	stillOccupied       string // "端口 %d 仍被占用（可能处于 TIME_WAIT 状态），请等待或换端口"
	noPermission        string // "权限不足，无法终止进程。请手动执行："
	sudoCmd             string // "  macOS/Linux:  sudo kill -9 %d"
	queryHint           string // "通过端口查询占用进程:"
	altPort             string // "或使用其他端口启动:  dqex --port %d"
	cancelled           string // "启动取消。"
}

func portTextsFor(lang string) portTexts {
	if lang == "en" {
		return enPortTexts
	}
	return zhPortTexts
}

var zhPortTexts = portTexts{
	portOccupied:        "端口 %d 已被占用:",
	portOccupiedUnknown: "端口 %d 已被占用，但无法识别占用进程。",
	procLine:            "  PID %d  %s",
	promptKill:          "是否终止占用进程? [y/N] ",
	killing:             "正在终止进程 %d ...",
	freed:               "端口 %d 已释放，正在启动服务...",
	stillOccupied:       "端口 %d 仍被占用（可能处于 TIME_WAIT 状态），请等待或换端口",
	noPermission:        "权限不足，无法终止进程。请手动执行：",
	sudoCmd:             "  macOS/Linux:  sudo kill -9 %d",
	queryHint:           "通过端口查询占用进程:",
	altPort:             "或使用其他端口启动:  dqex --port %d",
	cancelled:           "启动取消。",
}

var enPortTexts = portTexts{
	portOccupied:        "Port %d is occupied:",
	portOccupiedUnknown: "Port %d is occupied, but the occupying process could not be identified.",
	procLine:            "  PID %d  %s",
	promptKill:          "Terminate the occupying process? [y/N] ",
	killing:             "Terminating process %d ...",
	freed:               "Port %d released, starting service...",
	stillOccupied:       "Port %d is still occupied (possibly TIME_WAIT), please wait or use another port",
	noPermission:        "Insufficient permissions to terminate the process. Please run manually:",
	sudoCmd:             "  macOS/Linux:  sudo kill -9 %d",
	queryHint:           "Query occupying process by port:",
	altPort:             "Or use another port:  dqex --port %d",
	cancelled:           "Startup cancelled.",
}
