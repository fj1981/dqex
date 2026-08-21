package sqlcmd

// 离线SQL智能辅助：规则引擎 + 模板库
// 适用场景：客户现场无网络环境，无法调用LLM API

import (
	"fmt"
	"strings"
)

// SQLTemplate 预定义SQL模板
type SQLTemplate struct {
	ID          string   // 模板ID (如 "top_n", "recent_days")
	Name        string   // 模板名称 (中文/英文)
	Description string   // 模板描述
	Pattern     string   // SQL模式 (含占位符 {table}, {column}, {n}, {days})
	Example     string   // 使用示例
	Category    string   // 分类: query/aggregation/filter/join
}

// OfflineAssistant 离线SQL助手
type OfflineAssistant struct {
	templates map[string]SQLTemplate
	tableCache []string  // 当前库的表列表
}

// NewOfflineAssistant 创建离线助手实例
func NewOfflineAssistant() *OfflineAssistant {
	return &OfflineAssistant{
		templates: buildTemplateLibrary(),
	}
}

// buildTemplateLibrary 构建模板库 (50+ 常用查询模板)
func buildTemplateLibrary() map[string]SQLTemplate {
	return map[string]SQLTemplate{
		// ===== 基础查询类 =====
		"select_all": {
			ID:          "select_all",
			Name:        "全表查询",
			Description: "查询表中所有数据（带LIMIT限制）",
			Pattern:     "SELECT * FROM {table} LIMIT {n}",
			Example:     "\\template select_all users 100",
			Category:    "query",
		},
		"select_columns": {
			ID:          "select_columns",
			Name:        "指定列查询",
			Description: "查询表的指定列",
			Pattern:     "SELECT {columns} FROM {table} LIMIT {n}",
			Example:     "\\template select_columns \"id,name,email\" users 100",
			Category:    "query",
		},
		
		// ===== 聚合统计类 =====
		"count_rows": {
			ID:          "count_rows",
			Name:        "行数统计",
			Description: "统计表总行数",
			Pattern:     "SELECT COUNT(*) AS total FROM {table}",
			Example:     "\\template count_rows users",
			Category:    "aggregation",
		},
		"group_by_count": {
			ID:          "group_by_count",
			Name:        "分组计数",
			Description: "按指定列分组并计数",
			Pattern:     "SELECT {column}, COUNT(*) AS cnt FROM {table} GROUP BY {column} ORDER BY cnt DESC",
			Example:     "\\template group_by_count status orders",
			Category:    "aggregation",
		},
		"top_n": {
			ID:          "top_n",
			Name:        "Top N 查询",
			Description: "查询某列值最大的前N条记录",
			Pattern:     "SELECT * FROM {table} ORDER BY {column} DESC LIMIT {n}",
			Example:     "\\template top_n amount orders 10",
			Category:    "aggregation",
		},
		"bottom_n": {
			ID:          "bottom_n",
			Name:        "Bottom N 查询",
			Description: "查询某列值最小的前N条记录",
			Pattern:     "SELECT * FROM {table} ORDER BY {column} ASC LIMIT {n}",
			Example:     "\\template bottom_n price products 10",
			Category:    "aggregation",
		},
		"avg_value": {
			ID:          "avg_value",
			Name:        "平均值统计",
			Description: "计算某列的平均值",
			Pattern:     "SELECT AVG({column}) AS avg_val FROM {table}",
			Example:     "\\template avg_value salary employees",
			Category:    "aggregation",
		},
		"sum_value": {
			ID:          "sum_value",
			Name:        "求和统计",
			Description: "计算某列的总和",
			Pattern:     "SELECT SUM({column}) AS total FROM {table}",
			Example:     "\\template sum_value amount orders",
			Category:    "aggregation",
		},
		
		// ===== 时间范围类 =====
		"recent_days": {
			ID:          "recent_days",
			Name:        "最近N天数据",
			Description: "查询最近N天的记录",
			Pattern:     "SELECT * FROM {table} WHERE {date_column} >= DATE_SUB(NOW(), INTERVAL {days} DAY) ORDER BY {date_column} DESC",
			Example:     "\\template recent_days orders created_at 30",
			Category:    "filter",
		},
		"this_month": {
			ID:          "this_month",
			Name:        "本月数据",
			Description: "查询本月的记录",
			Pattern:     "SELECT * FROM {table} WHERE {date_column} >= DATE_FORMAT(NOW(), '%Y-%m-01') ORDER BY {date_column} DESC",
			Example:     "\\template this_month orders created_at",
			Category:    "filter",
		},
		"date_range": {
			ID:          "date_range",
			Name:        "日期范围查询",
			Description: "查询指定日期范围的记录",
			Pattern:     "SELECT * FROM {table} WHERE {date_column} BETWEEN '{start_date}' AND '{end_date}'",
			Example:     "\\template date_range orders created_at 2024-01-01 2024-12-31",
			Category:    "filter",
		},
		
		// ===== 条件过滤类 =====
		"where_equal": {
			ID:          "where_equal",
			Name:        "等值过滤",
			Description: "查询某列等于指定值的记录",
			Pattern:     "SELECT * FROM {table} WHERE {column} = '{value}' LIMIT {n}",
			Example:     "\\template where_equal users status active 100",
			Category:    "filter",
		},
		"where_like": {
			ID:          "where_like",
			Name:        "模糊查询",
			Description: "LIKE 模糊匹配查询",
			Pattern:     "SELECT * FROM {table} WHERE {column} LIKE '%{value}%' LIMIT {n}",
			Example:     "\\template where_like users name 张 100",
			Category:    "filter",
		},
		"where_in": {
			ID:          "where_in",
			Name:        "IN 条件查询",
			Description: "查询某列在指定值列表中的记录",
			Pattern:     "SELECT * FROM {table} WHERE {column} IN ({values}) LIMIT {n}",
			Example:     "\\template where_in users status \"'active','pending'\" 100",
			Category:    "filter",
		},
		"where_between": {
			ID:          "where_between",
			Name:        "范围查询",
			Description: "BETWEEN 范围查询",
			Pattern:     "SELECT * FROM {table} WHERE {column} BETWEEN {min} AND {max}",
			Example:     "\\template where_between products price 10 100",
			Category:    "filter",
		},
		
		// ===== JOIN 关联查询类 =====
		"join_two_tables": {
			ID:          "join_two_tables",
			Name:        "两表JOIN查询",
			Description: "INNER JOIN 两张表",
			Pattern:     "SELECT * FROM {table1} t1 INNER JOIN {table2} t2 ON t1.{fk_column} = t2.{pk_column} LIMIT {n}",
			Example:     "\\template join_two_tables orders users user_id id 100",
			Category:    "join",
		},
		"left_join": {
			ID:          "left_join",
			Name:        "LEFT JOIN查询",
			Description: "左连接两张表",
			Pattern:     "SELECT * FROM {table1} t1 LEFT JOIN {table2} t2 ON t1.{fk_column} = t2.{pk_column} LIMIT {n}",
			Example:     "\\template left_join users orders user_id user_id 100",
			Category:    "join",
		},
		
		// ===== 数据质量检查类 =====
		"check_null": {
			ID:          "check_null",
			Name:        "空值检查",
			Description: "查询某列为NULL的记录",
			Pattern:     "SELECT * FROM {table} WHERE {column} IS NULL LIMIT {n}",
			Example:     "\\template check_null users email 100",
			Category:    "filter",
		},
		"check_duplicate": {
			ID:          "check_duplicate",
			Name:        "重复值检查",
			Description: "检查某列是否有重复值",
			Pattern:     "SELECT {column}, COUNT(*) AS cnt FROM {table} GROUP BY {column} HAVING cnt > 1 ORDER BY cnt DESC",
			Example:     "\\template check_duplicate users email",
			Category:    "aggregation",
		},
		
		// ===== 性能优化类 =====
		"explain_query": {
			ID:          "explain_query",
			Name:        "执行计划分析",
			Description: "查看SQL执行计划",
			Pattern:     "EXPLAIN SELECT * FROM {table} WHERE {column} = '{value}'",
			Example:     "\\template explain_query users status active",
			Category:    "query",
		},
		"suggest_index": {
			ID:          "suggest_index",
			Name:        "索引建议",
			Description: "根据WHERE条件建议索引",
			Pattern:     "-- 建议在 {table}.{column} 上创建索引\nCREATE INDEX idx_{table}_{column} ON {table}({column});",
			Example:     "\\template suggest_index users email",
			Category:    "query",
		},
	}
}

// ApplyTemplate 应用模板生成SQL
func (oa *OfflineAssistant) ApplyTemplate(templateID string, args []string) (string, error) {
	tmpl, ok := oa.templates[templateID]
	if !ok {
		return "", fmt.Errorf("模板 '%s' 不存在，使用 \\templates 查看所有可用模板", templateID)
	}
	
	sql := tmpl.Pattern
	
	// 根据模板类型解析参数
	switch templateID {
	case "select_all", "count_rows", "this_month":
		// 简单模板：只需要表名
		if len(args) < 1 {
			return "", fmt.Errorf("用法: \\template %s <表名> [其他参数]", templateID)
		}
		sql = strings.ReplaceAll(sql, "{table}", args[0])
		if strings.Contains(sql, "{n}") && len(args) > 1 {
			sql = strings.ReplaceAll(sql, "{n}", args[1])
		} else {
			sql = strings.ReplaceAll(sql, "{n}", "100")
		}
		
	case "top_n", "bottom_n", "group_by_count":
		// 需要表名 + 列名
		if len(args) < 2 {
			return "", fmt.Errorf("用法: \\template %s <列名> <表名> [N]", templateID)
		}
		sql = strings.ReplaceAll(sql, "{column}", args[0])
		sql = strings.ReplaceAll(sql, "{table}", args[1])
		if len(args) > 2 {
			sql = strings.ReplaceAll(sql, "{n}", args[2])
		} else {
			sql = strings.ReplaceAll(sql, "{n}", "10")
		}
		
	case "recent_days":
		// 需要表名 + 日期列 + 天数
		if len(args) < 2 {
			return "", fmt.Errorf("用法: \\template %s <表名> <日期列> [天数]", templateID)
		}
		sql = strings.ReplaceAll(sql, "{table}", args[0])
		sql = strings.ReplaceAll(sql, "{date_column}", args[1])
		if len(args) > 2 {
			sql = strings.ReplaceAll(sql, "{days}", args[2])
		} else {
			sql = strings.ReplaceAll(sql, "{days}", "30")
		}
		
	case "where_equal", "where_like":
		// 需要表名 + 列名 + 值
		if len(args) < 3 {
			return "", fmt.Errorf("用法: \\template %s <表名> <列名> <值> [N]", templateID)
		}
		sql = strings.ReplaceAll(sql, "{table}", args[0])
		sql = strings.ReplaceAll(sql, "{column}", args[1])
		sql = strings.ReplaceAll(sql, "{value}", args[2])
		if len(args) > 3 {
			sql = strings.ReplaceAll(sql, "{n}", args[3])
		} else {
			sql = strings.ReplaceAll(sql, "{n}", "100")
		}
		
	default:
		// 通用替换逻辑
		for i, arg := range args {
			placeholder := fmt.Sprintf("{arg%d}", i)
			sql = strings.ReplaceAll(sql, placeholder, arg)
		}
	}
	
	return sql, nil
}

// ListTemplates 列出所有可用模板
func (oa *OfflineAssistant) ListTemplates(category string) []SQLTemplate {
	var result []SQLTemplate
	for _, tmpl := range oa.templates {
		if category == "" || tmpl.Category == category {
			result = append(result, tmpl)
		}
	}
	return result
}

// SuggestTemplate 根据用户输入推荐模板
func (oa *OfflineAssistant) SuggestTemplate(input string) []SQLTemplate {
	input = strings.ToLower(strings.TrimSpace(input))
	var suggestions []SQLTemplate
	
	// 关键词匹配
	keywords := map[string][]string{
		"top":        {"top_n"},
		"最近":       {"recent_days", "this_month"},
		"recent":     {"recent_days", "this_month"},
		"统计":       {"count_rows", "group_by_count", "avg_value", "sum_value"},
		"count":      {"count_rows", "group_by_count"},
		"平均":       {"avg_value"},
		"avg":        {"avg_value"},
		"重复":       {"check_duplicate"},
		"duplicate":  {"check_duplicate"},
		"空值":       {"check_null"},
		"null":       {"check_null"},
		"索引":       {"suggest_index", "explain_query"},
		"index":      {"suggest_index", "explain_query"},
		"join":       {"join_two_tables", "left_join"},
		"关联":       {"join_two_tables", "left_join"},
	}
	
	for keyword, templateIDs := range keywords {
		if strings.Contains(input, keyword) {
			for _, id := range templateIDs {
				if tmpl, ok := oa.templates[id]; ok {
					suggestions = append(suggestions, tmpl)
				}
			}
		}
	}
	
	// 如果没有匹配，返回最常用的5个模板
	if len(suggestions) == 0 {
		popularIDs := []string{"select_all", "count_rows", "top_n", "recent_days", "where_equal"}
		for _, id := range popularIDs {
			if tmpl, ok := oa.templates[id]; ok {
				suggestions = append(suggestions, tmpl)
			}
		}
	}
	
	return suggestions
}
