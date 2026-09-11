package engine

import (
	"reflect"
	"testing"
)

// 白名单条目解析与库归属筛选（表计划构建的纯逻辑部分）；
// 存在性校验依赖 cli.IsTableExist（见 conn.go resolveTablesByWhitelist / locateWhitelistedTable）
func TestEntriesForDB(t *testing.T) {
	cases := []struct {
		name   string
		wanted []string
		db     string
		want   []string // 期望保留的条目原文序列
	}{
		{"nil 无条目", nil, "mydb", []string{}},
		{"空数组无条目", []string{}, "mydb", []string{}},
		{"三级保留", []string{"mydb.sales.users"}, "mydb", []string{"mydb.sales.users"}},
		{"三级库不匹配", []string{"otherdb.sales.users"}, "mydb", []string{}},
		{"二级保留", []string{"mydb.users"}, "mydb", []string{"mydb.users"}},
		{"二级库不匹配", []string{"otherdb.users"}, "mydb", []string{}},
		{"裸名任意库", []string{"users"}, "mydb", []string{"users"}},
		{"大小写不敏感", []string{"MYDB.SALES.USERS"}, "mydb", []string{"MYDB.SALES.USERS"}},
		{"四段不支持", []string{"a.b.c.d"}, "mydb", []string{}},
		{"空串跳过", []string{"", "mydb.users"}, "mydb", []string{"mydb.users"}},
	}
	for _, c := range cases {
		entries := entriesForDB(c.wanted, c.db)
		got := make([]string, 0, len(entries))
		for _, e := range entries {
			got = append(got, e.raw)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: entriesForDB(%v, %q) = %v, want %v", c.name, c.wanted, c.db, got, c.want)
		}
	}
}

// 条目字段解析：裸名/库段/schema 段大小写归一，原文保留
func TestParseTableWanted(t *testing.T) {
	e, ok := parseTableWanted("MyDB.Sales.Users")
	if !ok || e.bare != "users" || e.bareRaw != "Users" || e.schema != "sales" || e.db != "mydb" || e.raw != "MyDB.Sales.Users" {
		t.Fatalf("parseTableWanted 三级 = %+v, ok=%v", e, ok)
	}
	e, ok = parseTableWanted("mydb.users")
	if !ok || e.bare != "users" || e.db != "mydb" || e.schema != "" {
		t.Fatalf("parseTableWanted 二级 = %+v, ok=%v", e, ok)
	}
	e, ok = parseTableWanted("users")
	if !ok || e.bare != "users" || e.db != "" || e.schema != "" {
		t.Fatalf("parseTableWanted 裸名 = %+v, ok=%v", e, ok)
	}
	if _, ok = parseTableWanted("a.b.c.d"); ok {
		t.Fatalf("四段应不支持")
	}
	if _, ok = parseTableWanted("  "); ok {
		t.Fatalf("空串应不支持")
	}
}

// 条件表名匹配：三级精确 / 二级任意 schema / 裸名任意库
func TestFindConditionPgSchema(t *testing.T) {
	conds := []TableCondition{
		{TableName: "mydb.sales.users"},
		{TableName: "mydb.orders"},
		{TableName: "logs"},
	}
	if c := findCondition(conds, "mydb", "sales.users"); c == nil || c.TableName != "mydb.sales.users" {
		t.Fatalf("三级匹配失败: %+v", c)
	}
	if c := findCondition(conds, "mydb", "public.orders"); c == nil || c.TableName != "mydb.orders" {
		t.Fatalf("二级匹配失败: %+v", c)
	}
	if c := findCondition(conds, "mydb", "other.logs"); c == nil || c.TableName != "logs" {
		t.Fatalf("裸名匹配失败: %+v", c)
	}
	if c := findCondition(conds, "mydb", "sales.unknown"); c != nil {
		t.Fatalf("不应匹配: %+v", c)
	}
}

// 备份表名：限定名保留 schema 前缀，仅对表名段加前缀
func TestBackupTableNameSchema(t *testing.T) {
	if got := backupTableName("sales.users"); got != "sales."+BackupTablePrefix+"users" {
		t.Fatalf("backupTableName(sales.users) = %s, want sales.%susers", got, BackupTablePrefix)
	}
	if got := backupTableName("users"); got != BackupTablePrefix+"users" {
		t.Fatalf("backupTableName(users) = %s, want %susers", got, BackupTablePrefix)
	}
}
