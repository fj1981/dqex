package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/fj1981/infrakit/pkg/cydb"
	"github.com/fj1981/infrakit/pkg/cydb/ss"
)

// TestBuildFilterWheres 验证过滤条件转换：操作符映射、错误处理、大字段限制。
func TestBuildFilterWheres(t *testing.T) {
	// 正常转换：所有操作符
	t.Run("操作符映射", func(t *testing.T) {
		filters := []ColumnFilter{
			{Column: "id", Op: FilterEq, Value: 1},
			{Column: "name", Op: FilterContains, Value: "abc"},
			{Column: "age", Op: FilterGte, Value: 18},
			{Column: "deleted", Op: FilterIsNull},
		}
		conds, err := buildFilterWheres(filters, nil)
		if err != nil {
			t.Fatalf("buildFilterWheres error = %v", err)
		}
		if len(conds) != 4 {
			t.Fatalf("conds len = %d, want 4", len(conds))
		}
	})

	// 未知操作符拒绝
	t.Run("未知操作符拒绝", func(t *testing.T) {
		_, err := buildFilterWheres([]ColumnFilter{{Column: "a", Op: "drop"}}, nil)
		if err == nil {
			t.Error("未知操作符应报错")
		}
	})

	// 空列名拒绝
	t.Run("空列名拒绝", func(t *testing.T) {
		_, err := buildFilterWheres([]ColumnFilter{{Column: "", Op: FilterEq, Value: 1}}, nil)
		if err == nil {
			t.Error("空列名应报错")
		}
	})

	// 大字段列限制：仅允许 isNull/isNotNull
	t.Run("大字段列限制", func(t *testing.T) {
		isBig := func(col string) bool { return col == "blob_col" }
		if _, err := buildFilterWheres([]ColumnFilter{{Column: "blob_col", Op: FilterEq, Value: "x"}}, isBig); err == nil {
			t.Error("大字段列 eq 应报错")
		}
		if _, err := buildFilterWheres([]ColumnFilter{{Column: "blob_col", Op: FilterIsNull}}, isBig); err != nil {
			t.Errorf("大字段列 isNull 不应报错: %v", err)
		}
	})
}

// TestBuildFilterWheresSQLInjection 验证注入 payload 被参数化绑定，不产生 SQL 拼接。
// 关键安全断言：值绝不内联进 SQL 文本，而是作为独立参数（由驱动负责转义）。
// 注意：contains 语义下 cydb 会自动包裹 %...% 并转义 %/_（EscapeLikePattern），
// 故参数值 = "%" + 转义后值 + "%"，这是预期行为而非漏洞。
func TestBuildFilterWheresSQLInjection(t *testing.T) {
	payloads := []string{
		"' OR '1'='1",
		"'; DROP TABLE users; --",
		"1' OR 1=1 --",
		`" OR ""="`,
		"%' OR 1=1 OR '%'='",
	}
	for _, p := range payloads {
		// 用 eq（非 LIKE）验证值原样参数化，不引入 LIKE 包裹干扰
		conds, err := buildFilterWheres([]ColumnFilter{{Column: "name", Op: FilterEq, Value: p}}, nil)
		if err != nil {
			t.Fatalf("payload %q: buildFilterWheres error = %v", p, err)
		}
		q := ss.Q().From("users").SelectIfEmpty(ss.Star()).Where(cydb.AND(conds...))
		sql, args, err := q.BuildMySQL()
		if err != nil {
			t.Fatalf("payload %q: BuildMySQL error = %v", p, err)
		}
		// 值必须是参数占位符，不能出现在 SQL 文本中（防拼接注入）
		if strings.Contains(sql, p) {
			t.Errorf("payload %q: 值被内联进 SQL，存在注入风险: %s", p, sql)
		}
		if len(args) != 1 {
			t.Errorf("payload %q: args len = %d, want 1", p, len(args))
		}
		// eq 语义下参数值应原样保留（无 LIKE 包裹）
		if got, ok := args[0].(string); !ok || got != p {
			t.Errorf("payload %q: 参数值 = %v, 应原样保留", p, args[0])
		}
	}
}

// TestBuildFilterWheresLIKE 验证 LIKE 操作符：值被参数化，且通配符 %/_ 被 cydb 自动转义。
// 这是安全性关键点：用户输入的 % 若不被转义会破坏 LIKE 语义（通配注入）。
func TestBuildFilterWheresLIKE(t *testing.T) {
	conds, err := buildFilterWheres([]ColumnFilter{{Column: "name", Op: FilterContains, Value: "100%"}}, nil)
	if err != nil {
		t.Fatalf("buildFilterWheres error = %v", err)
	}
	q := ss.Q().From("t").SelectIfEmpty(ss.Star()).Where(cydb.AND(conds...))
	sql, args, err := q.BuildMySQL()
	if err != nil {
		t.Fatalf("BuildMySQL error = %v", err)
	}
	// 值走参数化，SQL 中不应出现字面 100%
	if strings.Contains(sql, "100%") {
		t.Errorf("LIKE 值未参数化: %s", sql)
	}
	if len(args) != 1 {
		t.Fatalf("args len = %d, want 1", len(args))
	}
	// contains 语义：参数值 = "%100\%%"（cydb 包裹 %...% 并把用户输入的 % 转义为 \%）
	got := args[0].(string)
	if !strings.HasPrefix(got, "%") || !strings.HasSuffix(got, "%") {
		t.Errorf("contains 参数值 %q 应包裹 %%...%%", got)
	}
	// 用户输入的 % 必须被转义为 \%（在包裹 % 内部）
	inner := got[1 : len(got)-1]
	if !strings.Contains(inner, `\%`) {
		t.Errorf("用户输入的通配符 %% 未被转义: %q", got)
	}
}

// TestRunParamInsertValidation 验证 INSERT 参数校验（无需数据库连接即可触发）。
func TestRunParamInsertValidation(t *testing.T) {
	// 空表名
	if _, err := RunParamInsert(context.Background(), nil, InsertRowParams{Columns: []string{"a"}, Values: []any{1}}); err == nil {
		t.Error("空表名应报错")
	}
	// 列与值数量不一致
	if _, err := RunParamInsert(context.Background(), nil, InsertRowParams{Table: "t", Columns: []string{"a"}, Values: []any{1, 2}}); err == nil {
		t.Error("列与值数量不一致应报错")
	}
	// 空列
	if _, err := RunParamInsert(context.Background(), nil, InsertRowParams{Table: "t"}); err == nil {
		t.Error("空列应报错")
	}
}
