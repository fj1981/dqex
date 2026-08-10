package engine

import (
	"testing"
	"time"
)

// ---- 值归一化 ----

func TestNormalizeValueBasic(t *testing.T) {
	cases := []struct {
		name string
		a, b any
		want bool // 归一化后是否应相等
	}{
		{"nil与nil", nil, nil, true},
		{"nil与非nil", nil, "", false},
		{"bool", true, true, true},
		{"int与int64", 1, int64(1), true},
		{"数值不等", 1, 2, false},
		{"浮点精度噪声", 0.1 + 0.2, 0.3, true},
		{"负零统一", -0.0, 0.0, true},
		{"decimal字符串", "1.0", "1.00", true},
		{"decimal前导零", "007.50", "7.5", true},
		{"负零字符串", "-0", "0", true},
		{"数值字符串与数值", "42", int64(42), true},
		{"时间与时区字符串", time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), "2024-01-02 03:04:05", true},
		{"时间字符串与time.Time(带时区)", "2024-01-02T03:04:05Z", time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), true},
		{"不同时间", "2024-01-02 03:04:05", "2024-01-02 03:04:06", false},
		{"普通文本", "hello", "hello", true},
		{"文本不等", "hello", "world", false},
		{"byte文本与string", []byte("abc"), "abc", true},
		{"二进制hex", []byte{0x00, 0x01}, []byte{0x00, 0x01}, true},
		{"二进制不等", []byte{0x00, 0x01}, []byte{0x00, 0x02}, false},
		{"日期字符串", "2024-01-02", "2024-01-02", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := normalizeValue(c.a) == normalizeValue(c.b)
			if got != c.want {
				t.Errorf("normalizeValue(%v)=%q vs normalizeValue(%v)=%q, 相等=%v, 期望 %v",
					c.a, normalizeValue(c.a), c.b, normalizeValue(c.b), got, c.want)
			}
		})
	}
}

func TestCanonicalDecimal(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"1.0", "1", true},
		{"1.00", "1", true},
		{"007.50", "7.5", true},
		{"-0", "0", true},
		{"-0.00", "0", true},
		{"+3.10", "3.1", true},
		{".5", "0.5", true},
		{"100", "100", true},
		{"abc", "", false},
		{"1.2.3", "", false},
		{"2024-01-02", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := canonicalDecimal(c.in)
		if got != c.want || ok != c.wantOK {
			t.Errorf("canonicalDecimal(%q) = (%q, %v), 期望 (%q, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

// ---- 多重集差集 ----

func TestMultisetDiff(t *testing.T) {
	src := map[string]int{"a": 2, "b": 1, "c": 1}
	tgt := map[string]int{"a": 1, "b": 1, "d": 3}
	missing, extra := multisetDiff(src, tgt)
	if missing["a"] != 1 || missing["c"] != 1 || len(missing) != 2 {
		t.Errorf("missing 期望 {a:1 c:1}, 实际 %v", missing)
	}
	if extra["d"] != 3 || len(extra) != 1 {
		t.Errorf("extra 期望 {d:3}, 实际 %v", extra)
	}
	if sumCounts(missing) != 2 || sumCounts(extra) != 3 {
		t.Errorf("计数汇总错误: %d / %d", sumCounts(missing), sumCounts(extra))
	}

	// 完全一致
	m2, e2 := multisetDiff(src, src)
	if len(m2) != 0 || len(e2) != 0 {
		t.Errorf("相同多重集应无差异: %v / %v", m2, e2)
	}
}

func TestCollectSamplesDeterministic(t *testing.T) {
	diff := map[string]int{}
	rows := map[string]map[string]any{}
	for i := 0; i < 30; i++ {
		key := string(rune('a' + i))
		diff[key] = 1
		rows[key] = map[string]any{"id": i}
	}
	s1 := collectSamples(diff, rows)
	s2 := collectSamples(diff, rows)
	if len(s1) != compareSampleLimit {
		t.Fatalf("样本数期望 %d, 实际 %d", compareSampleLimit, len(s1))
	}
	for i := range s1 {
		if s1[i]["id"] != s2[i]["id"] {
			t.Fatalf("样本不确定: 第%d条 %v vs %v", i, s1[i], s2[i])
		}
	}
}

// ---- 别名配对构建 ----

func TestBuildComparePairs(t *testing.T) {
	src := []string{"users", "orders", "t_log", "src_only"}
	tgt := []string{"users", "orders", "tb_log", "tgt_only"}

	t.Run("同名匹配加别名", func(t *testing.T) {
		pairs, err := buildComparePairs(src, tgt, []TableAlias{{Source: "t_log", Target: "tb_log"}})
		if err != nil {
			t.Fatal(err)
		}
		// 别名配对1 + 同名匹配2(users/orders) + 仅源有1(src_only) + 仅目标有1(tgt_only)
		if len(pairs) != 5 {
			t.Fatalf("配对数期望 5, 实际 %d: %+v", len(pairs), pairs)
		}
		byName := map[string]comparePair{}
		for _, p := range pairs {
			byName[p.Name] = p
		}
		alias, ok := byName["t_log ↔ tb_log"]
		if !ok || alias.Status != compareStatusBoth {
			t.Errorf("别名配对缺失或状态错误: %+v", byName)
		}
		if byName["users"].Status != compareStatusBoth || byName["src_only"].Status != compareStatusSourceOnly || byName["tgt_only"].Status != compareStatusTargetOnly {
			t.Errorf("同名/单侧配对状态错误: %+v", byName)
		}
	})

	t.Run("大小写不敏感", func(t *testing.T) {
		pairs, err := buildComparePairs([]string{"USERS"}, []string{"users"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(pairs) != 1 || pairs[0].Status != compareStatusBoth {
			t.Errorf("大小写不敏感匹配失败: %+v", pairs)
		}
	})

	t.Run("别名目标不存在降级", func(t *testing.T) {
		pairs, err := buildComparePairs(src, tgt, []TableAlias{{Source: "t_log", Target: "not_exist"}})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, p := range pairs {
			if p.SourceName == "t_log" && p.Status == compareStatusSourceOnly {
				found = true
			}
			if p.Name == "t_log ↔ not_exist" {
				t.Errorf("目标不存在时不应产出 both 配对")
			}
		}
		if !found {
			t.Errorf("别名目标缺失应降级为 source_only: %+v", pairs)
		}
	})

	t.Run("重复配对报错", func(t *testing.T) {
		_, err := buildComparePairs(src, tgt, []TableAlias{
			{Source: "t_log", Target: "tb_log"},
			{Source: "t_log", Target: "orders"},
		})
		if err == nil {
			t.Error("同一源表参与多个别名配对应报错")
		}
	})

	t.Run("别名优先于同名", func(t *testing.T) {
		// 目标侧同时存在与源同名的 t_log，但别名将 t_log 指向 tb_log
		src2 := []string{"t_log"}
		tgt2 := []string{"t_log", "tb_log"}
		pairs, err := buildComparePairs(src2, tgt2, []TableAlias{{Source: "t_log", Target: "tb_log"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(pairs) != 2 {
			t.Fatalf("配对数期望 2, 实际 %+v", pairs)
		}
		for _, p := range pairs {
			if p.SourceName == "t_log" && p.TargetName != "tb_log" {
				t.Errorf("别名应优先于同名匹配: %+v", p)
			}
		}
	})
}

// ---- 列差异与公共列 ----

func TestDiffColumns(t *testing.T) {
	src := []ColumnItem{
		{Name: "id", DataType: "int", Nullable: false, PrimaryKey: true},
		{Name: "name", DataType: "varchar(64)", Nullable: true},
		{Name: "legacy", DataType: "text", Nullable: true},
	}
	tgt := []ColumnItem{
		{Name: "ID", DataType: "int", Nullable: false, PrimaryKey: true}, // 大小写不同应匹配
		{Name: "name", DataType: "varchar(128)", Nullable: true},         // 类型不同
		{Name: "extra", DataType: "int", Nullable: true},
	}
	d := diffColumns(src, tgt)
	if d.Matched {
		t.Error("存在差异时 Matched 应为 false")
	}
	if len(d.SourceOnly) != 1 || d.SourceOnly[0].Name != "legacy" {
		t.Errorf("SourceOnly 期望 [legacy], 实际 %+v", d.SourceOnly)
	}
	if len(d.TargetOnly) != 1 || d.TargetOnly[0].Name != "extra" {
		t.Errorf("TargetOnly 期望 [extra], 实际 %+v", d.TargetOnly)
	}
	if len(d.Different) != 1 || d.Different[0].Name != "name" {
		t.Errorf("Different 期望 [name], 实际 %+v", d.Different)
	}

	// 完全一致
	d2 := diffColumns(src, src)
	if !d2.Matched {
		t.Error("相同列应 Matched")
	}
}

func TestCommonColumns(t *testing.T) {
	src := []ColumnItem{{Name: "ID"}, {Name: "Name"}, {Name: "src_only"}}
	tgt := []ColumnItem{{Name: "id"}, {Name: "NAME"}, {Name: "tgt_only"}}
	got := commonColumns(src, tgt)
	if len(got) != 2 || got[0] != "id" || got[1] != "name" {
		t.Errorf("公共列期望 [id name], 实际 %v", got)
	}
	if len(commonColumns(src, []ColumnItem{{Name: "x"}})) != 0 {
		t.Error("无公共列时应返回空")
	}
}

// ---- 汇总 ----

func TestBuildCompareSummary(t *testing.T) {
	tables := []CompareTableResult{
		{Name: "a", Status: compareStatusBoth, Columns: &ColumnDiff{Matched: true}, Data: &DataDiff{Equal: true}},
		{Name: "b", Status: compareStatusBoth, Columns: &ColumnDiff{Matched: false}, Data: &DataDiff{Equal: true}},
		{Name: "c", Status: compareStatusBoth, Columns: &ColumnDiff{Matched: true}, Data: &DataDiff{Equal: false, Mode: "count"}},
		{Name: "d", Status: compareStatusSourceOnly},
		{Name: "e", Status: compareStatusTargetOnly},
	}
	s := buildCompareSummary(tables)
	want := CompareSummary{Total: 5, Matched: 1, SourceOnly: 1, TargetOnly: 1, StructureDiff: 1, DataDiff: 1}
	if s != want {
		t.Errorf("汇总期望 %+v, 实际 %+v", want, s)
	}
}

// ---- 裸表名过滤 ----

func TestFilterTablesBare(t *testing.T) {
	all := []string{"users", "orders"}
	if got := filterTablesBare(all, nil, "db1"); len(got) != 2 {
		t.Error("nil 应返回全部")
	}
	if got := filterTablesBare(all, []string{}, "db1"); len(got) != 0 {
		t.Error("空数组应返回空")
	}
	// 限定名剥离库前缀（库名匹配）
	if got := filterTablesBare(all, []string{"db1.users"}, "db1"); len(got) != 1 || got[0] != "users" {
		t.Errorf("限定名过滤失败: %v", got)
	}
	// 其他库的限定名不生效
	if got := filterTablesBare(all, []string{"other.users"}, "db1"); len(got) != 0 {
		t.Errorf("其他库限定名不应生效: %v", got)
	}
	// 大小写不敏感
	if got := filterTablesBare(all, []string{"USERS"}, "db1"); len(got) != 1 {
		t.Errorf("大小写不敏感过滤失败: %v", got)
	}
}
