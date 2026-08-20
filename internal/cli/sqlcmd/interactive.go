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
	txt := cliTextsFor(cliLang)
	sess, err := newSession(info)
	if err != nil {
		return textErr(err, cliTextsFor(cliLang).errConnDB)
	}
	defer sess.close()

	// 读取全局配置中的默认显示模式（config.yaml cli.display_mode），
	// 未配置时为 ""（等价 auto：表格超宽自动降级）
	if svc, err := newAIService(langCtx(), "", ""); err == nil {
		sess.displayMode = strings.TrimSpace(svc.Config().CLI.DisplayMode)
	}

	// 检查是否为终端环境
	if !isTerminal() {
		return textErr(nil, cliTextsFor(cliLang).errNeedTTY)
	}

	line := liner.NewLiner()
	defer line.Close()
	line.SetCtrlCAborts(true)

	histPath := historyFilePath()
	loadHistory(line, histPath)
	defer saveHistory(line, histPath)

	setupCompleter(line, sess)

	go sess.refreshMetadata()

	printf("%s\n", txt.bannerTitle)
	printf(txt.bannerConn+"\n", info.Type, info.Host, info.Port, info.DBName)
	printf("%s\n\n", txt.bannerHint)

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
				fprintf(os.Stderr, "\n%s\n", red(cliTextsFor(cliLang).needTTY))
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

		if strings.HasPrefix(input, "\\") || strings.HasPrefix(input, "/") {
			// 兼容 / 前缀习惯（如 /ai、/dt）：转换为 \ 前缀尝试元命令；
			// 未知命令（如 /* 注释开头）不处理，按原输入落入 SQL 缓冲区
			meta := input
			if strings.HasPrefix(input, "/") {
				meta = "\\" + input[1:]
			}
			handled, shouldQuit := sess.handleMeta(meta)
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
	txt := cliTextsFor(cliLang)
	// 无论成功失败，都先记录到历史和 lastSQL（带分号）
	s.lastSQL = sql
	line.AppendHistory(sql + ";")

	isWrite := classifySQL(sql)

	warnings, forbidden := checkDangerous(sql)
	if len(forbidden) > 0 {
		fprintf(os.Stderr, "%s\n", red(strings.Join(forbidden, "; ")))
		return
	}
	for _, w := range warnings {
		fprintf(os.Stderr, "%s\n", yellow(w))
	}

	if isWrite {
		fmt.Print(txt.confirmWrite)
		var answer string
		fmt.Scanln(&answer)
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Println(dim(txt.cancelled))
			return
		}
	}

	ctx := context.Background()
	start := time.Now()
	result, err := s.execute(ctx, sql)

	if err != nil {
		s.lastErr = err.Error()
		fprintf(os.Stderr, "%s\n", red(sprintf(txt.errorPrefix, err)))
		fmt.Fprintln(os.Stderr, dim(txt.aiFixHint))
		return
	}

	// 数据量警告
	if !isWrite && result.RowCount >= 1000 {
		limitInfo := ""
		if !hasLimitClause(sql) {
			limitInfo = sprintf(txt.autoLimit, defaultMaxRows)
		}
		fprintf(os.Stderr, "%s\n", yellow(sprintf(txt.rowsReturned, result.RowCount, limitInfo)))
		if result.RowCount >= 10000 {
			fmt.Fprintln(os.Stderr, dim(txt.rowsTip))
		}
	}

	if useVertical {
		s.renderResult(result, true)
	} else {
		s.renderResult(result, false)
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
	addr := sprintf("%s:%d/%s", s.connInfo.Host, s.connInfo.Port, s.currentDB)
	// liner 不支持 ANSI 颜色，用纯文本提示符避免光标错位
	return sprintf("dbx (%s @ %s) > ", dbType, addr)
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
