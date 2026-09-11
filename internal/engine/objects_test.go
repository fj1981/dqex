package engine

import (
	"reflect"
	"strings"
	"testing"
)

// TestObjectInWhitelist 对象白名单匹配：裸/库前缀/PG 三级条目 × 裸/schema 限定枚举名
func TestObjectInWhitelist(t *testing.T) {
	allowed := map[string]bool{
		"_functions/fn_mysql":         true, // 裸条目（CLI 手输）
		"rpa._functions/copy_auth":    true, // 库级裸名条目（rule 配置，MySQL/Oracle）
		"rpa.schema_a._views/v_order": true, // PG 三级条目（schema 限定）
		"rpa._procedures/sp_run":      true, // 库级裸名条目（PG 裸名 rule，依赖兜底匹配）
		"other._functions/fn_other":   true, // 其他库：验证库前缀隔离
	}
	tests := []struct {
		name string
		db   string
		id   string // objectWhitelistID 产物：目录/名 或 schema.目录/名
		want bool
	}{
		{"裸条目命中裸枚举", "rpa", "_functions/fn_mysql", true},
		{"库级条目命中裸枚举", "rpa", "_functions/copy_auth", true},
		{"PG schema 限定枚举 + 三级条目", "rpa", "schema_a._views/v_order", true},
		{"PG schema 限定枚举 + 库级裸条目兜底", "rpa", "schema_b._procedures/sp_run", true},
		{"库隔离：其他库条目不命中", "rpa", "_functions/fn_other", false},
		{"未配置对象不命中", "rpa", "_functions/fn_missing", false},
		{"PG 限定枚举未配置不命中", "rpa", "schema_b._functions/fn_missing", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := objectInWhitelist(allowed, tt.db, tt.id); got != tt.want {
				t.Fatalf("objectInWhitelist(%q, %q) = %v, want %v", tt.db, tt.id, got, tt.want)
			}
		})
	}
}

// TestMissingWhitelistObjects 缺失对象校验：命中条目不告警、缺失条目告警、跨库条目与重复条目不误报
func TestMissingWhitelistObjects(t *testing.T) {
	objs := dbObjects{
		objectView:     {"schema_a.v_order"}, // PG 限定枚举
		objectFunction: {"copy_auth"},        // 裸枚举（MySQL/Oracle）
	}
	objects := []string{
		"rpa.schema_a._views/v_order", // 命中
		"rpa._functions/copy_auth",    // 命中
		"rpa._functions/fn_missing",   // 缺失 → 告警
		"rpa._functions/fn_missing",   // 重复条目去重
		"other._functions/fn_other",   // 跨库条目不在此库校验
		"_functions/fn_bare",          // 裸条目归属任意库 → 告警
	}
	allowed := make(map[string]bool, len(objects))
	for _, o := range objects {
		allowed[strings.TrimSpace(o)] = true
	}
	got := missingWhitelistObjects(objects, allowed, objs, "rpa")
	want := []string{"rpa._functions/fn_missing", "_functions/fn_bare"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missingWhitelistObjects = %v, want %v", got, want)
	}
}
