// SQL 文本通用处理：库引擎无关的纯文本操作，供引擎内部与根包公开 API 共用。
package engine

import (
	"regexp"
	"strings"
)

// StripSQLDBPrefix 清除 SQL 中的源库名前缀（`db`. / db.），使外部生成的 SQL 可在目标库上执行。
// 前缀仅当命中 knownDBs（包内库名/目标库名）时清除，避免误伤别名前缀（t.col）；
// 非 MySQL 方言同时移除全部反引号（MySQL 专有语法，PG/Oracle 上非法）。
// 已知局限：与库名同形的字符串字面量会被误改（外部 SQL 场景可忽略，与 tl-env 原实现一致）。
func StripSQLDBPrefix(isMySQL bool, sql string, knownDBs ...string) string {
	if !isMySQL {
		sql = strings.ReplaceAll(sql, "`", "")
	}
	alt := make([]string, 0, len(knownDBs))
	for _, db := range knownDBs {
		if db != "" {
			alt = append(alt, regexp.QuoteMeta(db))
		}
	}
	if len(alt) == 0 {
		return sql
	}
	// 库名前缀要求"行首/非标识符字符"开头，防止误切 rpa_csx. 这类更长标识符的片段；
	// 后边界捕获一个标识符字符来替代 (?=...) lookahead（Go regexp 不支持前瞻断言）。
	// 匹配：前边界 + [引号]?库名[引号]? + . + 后续标识符字符 → 替换为 前边界 + 后续字符
	re, err := regexp.Compile(`(?i)(^|[^\w$])(["']?(?:` + strings.Join(alt, "|") + `)["']?)\.(["'\w$])`)
	if err != nil {
		return sql
	}
	return re.ReplaceAllString(sql, "$1$3")
}
