package engine

import (
	"reflect"
	"testing"
)

// PG 分层枚举后表清单为限定名 "schema.table"，wanted 支持三级/二级/裸名匹配
func TestFilterTablesPgSchema(t *testing.T) {
	all := []string{"public.users", "sales.users", "public.orders"}
	cases := []struct {
		name   string
		wanted []string
		want   []string
	}{
		{"nil 全部", nil, all},
		{"空数组无表", []string{}, []string{}},
		{"三级精确", []string{"mydb.sales.users"}, []string{"sales.users"}},
		{"三级库不匹配", []string{"otherdb.sales.users"}, []string{}},
		{"三级 schema 不匹配", []string{"mydb.public.users"}, []string{"public.users"}},
		{"二级任意 schema", []string{"mydb.users"}, []string{"public.users", "sales.users"}},
		{"裸名任意库 schema", []string{"users"}, []string{"public.users", "sales.users"}},
		{"大小写不敏感", []string{"MYDB.SALES.USERS"}, []string{"sales.users"}},
		{"四段不支持", []string{"a.b.c.d"}, []string{}},
	}
	for _, c := range cases {
		got := filterTables(all, c.wanted, "mydb")
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: filterTables(%v, %q) = %v, want %v", c.name, all, c.wanted, got, c.want)
		}
	}
}

// MySQL/Oracle 表清单为裸名，wanted 限定形式按裸名匹配
func TestFilterTablesBareList(t *testing.T) {
	all := []string{"users", "orders"}
	got := filterTables(all, []string{"mydb.users"}, "mydb")
	if !reflect.DeepEqual(got, []string{"users"}) {
		t.Fatalf("filterTables bare = %v, want [users]", got)
	}
	got = filterTables(all, []string{"mydb.orders"}, "otherdb")
	if len(got) != 0 {
		t.Fatalf("filterTables db mismatch = %v, want empty", got)
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
