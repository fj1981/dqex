package sqlcmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"dbimpex/internal/engine"

	"github.com/peterh/liner"
)

func runInteractive(info *engine.DBConnInfo) error {
	sess, err := newSession(info)
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}
	defer sess.close()

	// 检查是否为终端环境
	if !isTerminal() {
		return fmt.Errorf("交互模式需要终端环境，请使用 -e 执行 SQL 或 --json 输出")
	}

	line := liner.NewLiner()
	defer line.Close()
	line.SetCtrlCAborts(true)

	histPath := historyFilePath()
	loadHistory(line, histPath)
	defer saveHistory(line, histPath)

	setupCompleter(line, sess)

	go sess.refreshMetadata()

	fmt.Printf("dbx sql - 交互式 SQL 终端\n")
	fmt.Printf("连接: %s @ %s:%d/%s\n", info.Type, info.Host, info.Port, info.DBName)
	fmt.Printf("输入 \\h 查看帮助，\\q 退出\n\n")

	var buffer string
	for {
		prompt := sess.promptString()
		if buffer != "" {
			prompt = "  \u2192 "
		}

		input, err := line.Prompt(prompt)
		if err != nil {
			if err == liner.ErrPromptAborted {
				if buffer != "" {
					buffer = ""
					fmt.Println("^C")
					continue
				}
				fmt.Println()
				return nil
			}
			if err.Error() == "liner: internal error" {
				fmt.Fprintf(os.Stderr, "\n%s\n", red("交互模式需要真实终端，请使用 -e 或 --json"))
				return fmt.Errorf("liner: %w", err)
			}
			fmt.Println()
			return nil
		}

		input = strings.TrimSpace(input)

		if input == "" {
			if buffer == "" {
				continue
			}
			buffer += "\n"
			continue
		}

		if strings.HasPrefix(input, "\\") {
			handled, shouldQuit := sess.handleMeta(input)
			if shouldQuit {
				return nil
			}
			if handled {
				continue
			}
		}

		lower := strings.ToLower(input)
		if lower == "exit" || lower == "quit" {
			return nil
		}

		if buffer != "" {
			buffer += " " + input
		} else {
			buffer = input
		}

		if strings.HasSuffix(input, ";") || strings.HasSuffix(strings.ToUpper(input), "\\G") {
			rawSQL := strings.TrimSpace(buffer)
			useVertical := strings.HasSuffix(strings.ToUpper(rawSQL), "\\G")
			// 去掉结尾的 ; 或 \G
			sql := strings.TrimSuffix(rawSQL, ";")
			sql = strings.TrimSuffix(strings.TrimSpace(sql), "\\G")
			sql = strings.TrimSpace(sql)
			if sql != "" {
				sess.executeSQL(line, sql, useVertical)
			}
			buffer = ""
		}
	}
}

func (s *session) executeSQL(line *liner.State, sql string, useVertical bool) {
	// 无论成功失败，都先记录到历史和 lastSQL（带分号）
	s.lastSQL = sql
	line.AppendHistory(sql + ";")

	isWrite := classifySQL(sql)

	warnings, forbidden := checkDangerous(sql)
	if len(forbidden) > 0 {
		fmt.Fprintf(os.Stderr, "%s\n", red(strings.Join(forbidden, "; ")))
		return
	}
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "%s\n", yellow(w))
	}

	if isWrite {
		fmt.Printf("确认执行写操作? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Println(dim("已取消"))
			return
		}
	}

	ctx := context.Background()
	start := time.Now()
	result, err := s.execute(ctx, sql)

	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", red(fmt.Sprintf("错误: %v", err)))
		return
	}

	// 数据量警告
	if !isWrite && result.RowCount >= 1000 {
		limitInfo := ""
		if !hasLimitClause(sql) {
			limitInfo = "（已自动追加 LIMIT " + fmt.Sprintf("%d", defaultMaxRows) + "）"
		}
		fmt.Fprintf(os.Stderr, "%s\n", yellow(fmt.Sprintf("⚠ 已返回 %d 行%s", result.RowCount, limitInfo)))
		if result.RowCount >= 10000 {
			fmt.Fprintln(os.Stderr, dim("提示：使用 LIMIT n 指定返回行数，或加 WHERE 条件缩小范围"))
		}
	}

	if useVertical {
		outputVertical(result)
	} else {
		maybePage(result)
	}

	if isWrite {
		auditWrite(&s.connInfo, sql, result.AffectedRows, time.Since(start))
	}
}

func (s *session) promptString() string {
	dbType := s.dbType
	if len(dbType) > 6 {
		dbType = dbType[:6]
	}
	addr := fmt.Sprintf("%s:%d/%s", s.connInfo.Host, s.connInfo.Port, s.currentDB)
	// liner 不支持 ANSI 颜色，用纯文本提示符避免光标错位
	return fmt.Sprintf("dbx (%s @ %s) > ", dbType, addr)
}

func (s *session) refreshMetadata() {
	tables, err := s.cli.GetTables(s.currentDB, nil, nil)
	if err != nil {
		return
	}
	s.tableCache = tables
}

// setupCompleter 设置 Tab 补全（含静默降级容错）。
func setupCompleter(line *liner.State, sess *session) {
	line.SetTabCompletionStyle(liner.TabCircular)
	line.SetCompleter(func(lineText string) (c []string) {
		// 静默降级：补全异常时仅返回关键字补全，不崩溃
		defer func() {
			if r := recover(); r != nil {
				c = nil
			}
		}()
		head, completions, _ := sess.complete(lineText, len(lineText))
		var result []string
		for _, comp := range completions {
			result = append(result, head+comp)
		}
		return result
	})
}

// isTerminal 检测 stdin 是否为终端。
func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
