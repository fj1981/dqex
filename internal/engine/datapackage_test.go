package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fj1981/infrakit/pkg/cydb"
	"github.com/fj1981/infrakit/pkg/cydb/def"
)

// ---------------------------------------------------------------------------
// 测试连接配置：默认本地 MySQL（root / 127.0.0.1:3306），可用环境变量覆盖。
// 连接失败（本地无 MySQL）时相关用例自动 Skip，不阻塞纯单元用例与 CI。
// ---------------------------------------------------------------------------

func testMySQLInfo(t *testing.T) DBConnInfo {
	t.Helper()
	port, err := strconv.Atoi(envOr("DQEX_TEST_MYSQL_PORT", "3317"))
	if err != nil {
		port = 3306
	}
	return DBConnInfo{DBConnection: def.DBConnection{
		Type: "mysql",
		Host: envOr("DQEX_TEST_MYSQL_HOST", "127.0.0.1"),
		Port: port,
		Un:   envOr("DQEX_TEST_MYSQL_USER", "root"),
		Pw:   envOr("DQEX_TEST_MYSQL_PASSWORD", "Pass@2025W0rd#1Q!"),
	}}
}

func envOr(key, defVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defVal
}

// newTestMySQLDB 连接本地 MySQL 并创建一次性测试库（dqex_test_<纳秒>），测试结束自动
// DROP。连接失败时 Skip（本地无 MySQL 环境）。
func newTestMySQLDB(t *testing.T) *cydb.DBCli {
	t.Helper()
	info := testMySQLInfo(t)
	dbName := fmt.Sprintf("dqex_test_%d", time.Now().UnixNano())

	anchor, err := ConnectDB(info, "mysql")
	if err != nil {
		t.Skipf("本地 MySQL 不可用，跳过集成用例: %v", err)
	}
	defer anchor.Close()
	if _, err := anchor.Execute(fmt.Sprintf(
		"CREATE DATABASE `%s` DEFAULT CHARACTER SET utf8mb4", dbName)); err != nil {
		t.Skipf("创建测试库失败，跳过集成用例: %v", err)
	}

	cli, err := ConnectDB(info, dbName)
	if err != nil {
		// 建库成功但连接失败：Skip 前清理测试库，避免残留
		if a, aerr := ConnectDB(info, "mysql"); aerr == nil {
			_, _ = a.Execute(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName))
			_ = a.Close()
		}
		t.Skipf("连接测试库失败，跳过集成用例: %v", err)
	}
	t.Cleanup(func() {
		_ = cli.Close()
		if a, err := ConnectDB(info, "mysql"); err == nil {
			_, _ = a.Execute(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName))
			_ = a.Close()
		}
	})
	return cli
}

// mustCreateTable 建测试表（已存在则忽略）
func mustCreateTable(t *testing.T, cli *cydb.DBCli, ddl string) {
	t.Helper()
	if _, err := cli.Execute(ddl); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "already exists") {
			t.Fatalf("建表失败: %v", err)
		}
	}
}

// recordHooks 注册 QueryHooks 收集回调（验证审计埋点）
type hookRecord struct {
	stmt   string
	costMs int64
	rows   int64
}

func ctxWithRecorder(t *testing.T) (context.Context, *[]hookRecord) {
	t.Helper()
	rec := &[]hookRecord{}
	ctx := CtxWithQueryHooks(context.Background(), &QueryHooks{
		OnQuery: func(ctx context.Context, connKey, stmt string, costMs, rows int64) {
			*rec = append(*rec, hookRecord{stmt: stmt, costMs: costMs, rows: rows})
		},
	})
	return ctx, rec
}

// ---------------------------------------------------------------------------
// 纯单元用例（无外部依赖）
// ---------------------------------------------------------------------------

// TestLoadDataPackageNumericPK 三轮修复回归（#1 major）：数字主键经 JSON 往返不失真。
// LoadDataPackage 必须用 UseNumber——默认 Unmarshal 将数字解码为 float64：
// ①超过 2^53 的整数（雪花 ID）精度丢失；②pkValues 的 %v 对大 float64 输出科学计数法
// （如 '1.2345678901e+10'），PG bigint 列 IN 匹配报错、MySQL 上索引失效。
func TestLoadDataPackageNumericPK(t *testing.T) {
	raw := `{"db":"biz","datas":[
		{"type":1,"table":"t1","pk":["id"],"data":[{"id":1234567890123456789,"name":"雪花ID行"},{"id":12345678901,"name":"大整数行"}]}
	]}`
	pkg, err := LoadDataPackage([]byte(raw))
	if err != nil {
		t.Fatalf("解析数据包失败: %v", err)
	}
	if len(pkg.Entries) != 1 || len(pkg.Entries[0].Data) != 2 {
		t.Fatalf("条目解析错误: %+v", pkg.Entries)
	}
	// pkValues 输出原始值字符串（json.Number 经 %v 输出原文，不走 float64
	// 精度/科学计数法路径）；转义统一由 quoteInList 完成（避免双重转义）
	ids := pkValues(pkg.Entries[0].Data[0], []string{"id"})
	if len(ids) != 1 || ids[0] != "1234567890123456789" {
		t.Fatalf("雪花 ID 主键失真: got %v", ids)
	}
	ids = pkValues(pkg.Entries[0].Data[1], []string{"id"})
	if len(ids) != 1 || ids[0] != "12345678901" {
		t.Fatalf("大整数主键科学计数法回归: got %v", ids)
	}
	// IN 子句：quoteInList 转义一次，不含科学计数法、不双重转义
	in := quoteInList("postgresql", [][]string{pkValues(pkg.Entries[0].Data[1], []string{"id"})})
	if strings.Contains(in, "e+") {
		t.Fatalf("IN 子句含科学计数法: %s", in)
	}
	if in != "('12345678901')" {
		t.Fatalf("IN 子句双重转义: %s", in)
	}
	// 含特殊字符的原始值经 quoteInList 只转义一次
	got := quoteInList("mysql", [][]string{pkValues(map[string]interface{}{"id": "it's"}, []string{"id"})})
	if got != "('it''s')" {
		t.Fatalf("MySQL 特殊字符转义错误: %s", got)
	}
}

// TestLoadDataPackageNullEntries 三轮修复回归（#2 major）：外部数据包 datas 数组含
// null 条目时不得 panic（此前 ApplyDataPackage 遍历直接解引用 e.Type）。
func TestLoadDataPackageNullEntries(t *testing.T) {
	pkg, err := LoadDataPackage([]byte(`{"db":"biz","datas":[null,{"type":1,"table":"t1","pk":["id"],"data":[{"id":1}]}]}`))
	if err != nil {
		t.Fatalf("解析数据包失败: %v", err)
	}
	if len(pkg.Entries) != 1 || pkg.Entries[0] == nil {
		t.Fatalf("null 条目未过滤: len=%d", len(pkg.Entries))
	}
	if pkg.Entries[0].Table != "t1" {
		t.Fatalf("剩余条目错位: %+v", pkg.Entries[0])
	}
	// 全 null：空包可安全进入 ApplyDataPackage 循环
	empty, err := LoadDataPackage([]byte(`{"db":"biz","datas":[null,null]}`))
	if err != nil {
		t.Fatalf("解析数据包失败: %v", err)
	}
	if len(empty.Entries) != 0 {
		t.Fatalf("全 null 包应过滤为空: len=%d", len(empty.Entries))
	}
}

// TestQuoteInList 验证修复 #1：PK 值转义（外部数据包不可信输入防注入）
func TestQuoteInList(t *testing.T) {
	// MySQL：单引号双写 + 反斜杠双写
	got := quoteInList("mysql", [][]string{{"it's"}, {`bs\row`}})
	want := "('it''s'),('bs\\\\row')"
	if got != want {
		t.Errorf("mysql 转义: got %s want %s", got, want)
	}
	// PG/Oracle：单引号双写，反斜杠保持字面量
	got = quoteInList("postgresql", [][]string{{`bs\row`}})
	if want = `('bs\row')`; got != want {
		t.Errorf("pg 转义: got %s want %s", got, want)
	}
	// 注入载荷：整体保持为单一字面量，不改变 WHERE 语义
	payload := "x') OR ('1'='1"
	got = quoteInList("mysql", [][]string{{payload}})
	want = "('x'') OR (''1''=''1')"
	if got != want {
		t.Errorf("注入载荷未正确转义: got %s", got)
	}
	if strings.Contains(got, payload) {
		t.Errorf("注入载荷被原样拼入: %s", got)
	}
	// 多列主键 / 空列表
	if got, want = quoteInList("mysql", [][]string{{"a", "b"}}), "('a','b')"; got != want {
		t.Errorf("多列主键: got %s want %s", got, want)
	}
	if got := quoteInList("mysql", nil); got != "" {
		t.Errorf("空列表应为空串, got %s", got)
	}
}

// TestWriteRollbackArtifact 验证修复 #3/#5/#10：产物命名、库上下文头、0600 权限、错误路径
func TestWriteRollbackArtifact(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "dump.json")
	sqls := []string{"DELETE FROM `t1` WHERE (`id`) IN (('a'));", "REPLACE INTO `t1` VALUES ('a','x');"}

	// 单库：契约命名 <名称>.rollback.sql，无 Database 头
	p, err := writeRollbackArtifact(input, "", sqls)
	if err != nil {
		t.Fatalf("单库产物写出失败: %v", err)
	}
	if !strings.HasSuffix(p, "dump.rollback.sql") {
		t.Errorf("单库产物命名错误: %s", p)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读产物失败: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "-- dqex rollback") || !strings.Contains(content, sqls[0]) {
		t.Errorf("产物内容不完整:\n%s", content)
	}
	if strings.Contains(content, "-- Database:") {
		t.Errorf("单库产物不应含 Database 头:\n%s", content)
	}
	// 0600 权限（产物含明文业务数据）
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("产物权限应为 0600, got %v", fi.Mode().Perm())
	}

	// 多库：按库分文件 <名称>.<db>.rollback.sql，带 Database 头
	p2, err := writeRollbackArtifact(input, "biz_db", []string{"DELETE FROM `t2`;"})
	if err != nil {
		t.Fatalf("多库产物写出失败: %v", err)
	}
	if !strings.HasSuffix(p2, "dump.biz_db.rollback.sql") {
		t.Errorf("多库产物命名错误: %s", p2)
	}
	if data, _ = os.ReadFile(p2); !strings.Contains(string(data), "-- Database: biz_db") {
		t.Errorf("多库产物缺 Database 头:\n%s", string(data))
	}

	// 错误路径：目标目录不存在返回错误（不再误报 errImpExec 第 0 块语义）
	if _, err := writeRollbackArtifact(filepath.Join(dir, "no_such_dir", "x.json"), "", sqls); err == nil {
		t.Errorf("目录不存在时应返回错误")
	}
}

// ---------------------------------------------------------------------------
// 集成用例（本地 MySQL；连接失败自动 Skip）
// ---------------------------------------------------------------------------

const testTableDDL = "CREATE TABLE `%s` (" +
	"`id` varchar(64) NOT NULL, `name` varchar(255) DEFAULT NULL, `age` int DEFAULT NULL, " +
	"PRIMARY KEY (`id`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"

// TestApplyDataPackageMySQL 验证修复 #1/#6/#7：含引号/反斜杠 PK 的导入、
// 建表回滚、旧值还原回放、审计钩子
func TestApplyDataPackageMySQL(t *testing.T) {
	cli := newTestMySQLDB(t)
	tbl := "dp_t1"
	mustCreateTable(t, cli, fmt.Sprintf(testTableDDL, tbl))

	// 预置旧行（验证回滚 REPLACE 旧行路径）
	if _, err := cli.Execute(fmt.Sprintf(
		"INSERT INTO `%s` VALUES ('old1','旧值',1)", tbl)); err != nil {
		t.Fatalf("预置旧行失败: %v", err)
	}

	pkg := &DataPackage{DB: "dp_test"}
	pkg.Add(tbl, DataEntry{
		Type: DataEntryCreateTable,
		SQL:  fmt.Sprintf(testTableDDL, tbl),
	})
	pkg.Add(tbl, DataEntry{
		Type: DataEntryUpsertData,
		PK:   []string{"id"},
		Data: []map[string]interface{}{
			// PK 含单引号/反斜杠（修复 #1 的注入面）
			{"id": "it's", "name": "O'Brien", "age": 2},
			{"id": `bs\row`, "name": `a\b`, "age": 3},
			{"id": "old1", "name": "新值", "age": 9},
		},
	})

	ctx, rec := ctxWithRecorder(t)
	res, err := ApplyDataPackage(ctx, cli, pkg, "test:conn")
	if err != nil {
		t.Fatalf("应用数据包失败: %v", err)
	}

	// 建表条目已存在被跳过：无 DROP 回滚（修复 #6 既有表语义）
	for _, rb := range res.RollbackSQL {
		if strings.Contains(rb, "DROP TABLE") {
			t.Errorf("表已存在时不应生成 DROP TABLE 回滚: %s", rb)
		}
	}
	// 行级回滚：1 DELETE + 1 REPLACE（REPLACE 仅为导入前已存在的旧行生成：
	// 预置的 old1 一行；新行 it's / bs\row 无旧行，仅靠 DELETE 清除）
	var deletes, replaces int
	for _, rb := range res.RollbackSQL {
		switch {
		case strings.HasPrefix(rb, "DELETE"):
			deletes++
		case strings.HasPrefix(rb, "REPLACE"):
			replaces++
		}
	}
	if deletes != 1 || replaces != 1 {
		t.Errorf("回滚收集错误: DELETE=%d REPLACE=%d (期望 1/1)\n%v", deletes, replaces, res.RollbackSQL)
	}

	// 导入生效：old1 为 upsert 更新（不新增），3 行；PK 含引号/反斜杠的行已写入
	n := countRows(t, cli, tbl)
	if n != 3 {
		t.Fatalf("导入后行数错误: got %d want 3", n)
	}
	name := queryScalar(t, cli, fmt.Sprintf("SELECT name FROM `%s` WHERE id='it''s'", tbl))
	if name != "O'Brien" {
		t.Errorf("单引号 PK 行写入错误: name=%q", name)
	}

	// 审计钩子（修复 #7）：DELETE/REPLACE 均有埋点且语句非空
	if len(*rec) == 0 {
		t.Fatalf("QueryHooks 未触发")
	}
	for _, r := range *rec {
		if strings.TrimSpace(r.stmt) == "" {
			t.Errorf("钩子收到空语句")
		}
		if r.costMs < 0 {
			t.Errorf("钩子 costMs 异常: %d", r.costMs)
		}
	}

	// 回滚产物回放：恢复到导入前状态（old1 还原"旧值"，其余行清空）
	for _, rb := range res.RollbackSQL {
		stmt := strings.TrimSuffix(rb, ";")
		if _, err := cli.Execute(stmt); err != nil {
			t.Fatalf("回滚语句执行失败 [%s]: %v", rb, err)
		}
	}
	if n = countRows(t, cli, tbl); n != 1 {
		t.Fatalf("回滚后行数错误: got %d want 1", n)
	}
	if name = queryScalar(t, cli, fmt.Sprintf("SELECT name FROM `%s` WHERE id='old1'", tbl)); name != "旧值" {
		t.Errorf("回滚未还原旧行: name=%q", name)
	}
}

// TestApplyDataPackageFailureKeepsRollback 验证修复 #2：失败时返回非 nil res，
// 已执行条目的回滚 SQL 仍可兜底（MySQL DML 事务回滚 + 产物兜底语义）
func TestApplyDataPackageFailureKeepsRollback(t *testing.T) {
	cli := newTestMySQLDB(t)
	tbl := "dp_fail"
	mustCreateTable(t, cli, fmt.Sprintf(testTableDDL, tbl))

	pkg := &DataPackage{DB: "dp_test"}
	pkg.Add(tbl, DataEntry{
		Type: DataEntryUpsertData,
		PK:   []string{"id"},
		Data: []map[string]interface{}{{"id": "a", "name": "x"}},
	})
	// 失败语句：更新不存在的表
	pkg.Add(tbl, DataEntry{Type: DataEntryExecSQL, Data: []map[string]interface{}{
		{"UPDATE `no_such_table` SET x=1": ""},
	}})

	res, err := ApplyDataPackage(context.Background(), cli, pkg, "test:conn")
	if err == nil {
		t.Fatalf("失败语句应返回错误")
	}
	if res == nil {
		t.Fatalf("修复 #2 回归：失败时应返回非 nil res（已收集回滚不得丢弃）")
	}
	var hasDelete bool
	for _, rb := range res.RollbackSQL {
		if strings.HasPrefix(rb, "DELETE") {
			hasDelete = true
		}
	}
	if !hasDelete {
		t.Errorf("失败时已执行条目的回滚丢失: %v", res.RollbackSQL)
	}
}

// TestApplyDataPackageExecSQLRollback 验证修复 #4：rollback 值类型校验、
// 多执行键确定性顺序、MySQL 列级回滚（tryColumnRollback 走权威 DDL）
func TestApplyDataPackageExecSQLRollback(t *testing.T) {
	cli := newTestMySQLDB(t)
	tbl := "dp_exec"
	mustCreateTable(t, cli, fmt.Sprintf(testTableDDL, tbl))

	// 1) ALTER 列级变更 + 空回滚：tryColumnRollback 从建表 DDL 生成 MODIFY COLUMN 还原
	pkg := &DataPackage{DB: "dp_test"}
	pkg.Add(tbl, DataEntry{Type: DataEntryExecSQL, Data: []map[string]interface{}{
		{fmt.Sprintf("ALTER TABLE `%s` MODIFY COLUMN `age` bigint NOT NULL DEFAULT 0", tbl): ""},
	}})
	res, err := ApplyDataPackage(context.Background(), cli, pkg, "test:conn")
	if err != nil {
		t.Fatalf("ALTER 应用失败: %v", err)
	}
	if len(res.RollbackSQL) != 1 || !strings.Contains(res.RollbackSQL[0], "MODIFY COLUMN") ||
		!strings.Contains(res.RollbackSQL[0], "age") {
		t.Errorf("列级回滚生成错误: %v / Unrollback=%v", res.RollbackSQL, res.Unrollback)
	}
	// 回滚片段应为变更前定义（int 可空、无 NOT NULL DEFAULT）
	if strings.Contains(res.RollbackSQL[0], "NOT NULL DEFAULT") {
		t.Errorf("回滚片段非变更前定义: %s", res.RollbackSQL[0])
	}

	// 2) rollback 值为非字符串：不得被当 SQL 收集（修复 #4）
	pkg2 := &DataPackage{DB: "dp_test"}
	pkg2.Add(tbl, DataEntry{Type: DataEntryExecSQL, Data: []map[string]interface{}{
		{"SELECT 1": 123},
	}})
	res2, err := ApplyDataPackage(context.Background(), cli, pkg2, "test:conn")
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if len(res2.Unrollback) != 1 || res2.Unrollback[0] != "SELECT 1" {
		t.Errorf("非字符串回滚应记入 Unrollback: %v", res2.Unrollback)
	}
	for _, rb := range res2.RollbackSQL {
		if strings.Contains(rb, "123") {
			t.Errorf("非字符串回滚值被当 SQL 收集: %s", rb)
		}
	}

	// 3) 多执行键：按字典序确定性执行（修复 #4 顺序可复现）
	pkg3 := &DataPackage{DB: "dp_test"}
	pkg3.Add(tbl, DataEntry{Type: DataEntryExecSQL, Data: []map[string]interface{}{
		{"SELECT 'b'": "", "SELECT 'a'": ""},
	}})
	_, rec := ctxWithRecorder(t)
	ctx := CtxWithQueryHooks(context.Background(), &QueryHooks{
		OnQuery: func(ctx context.Context, connKey, stmt string, costMs, rows int64) {
			*rec = append(*rec, hookRecord{stmt: stmt})
		},
	})
	if _, err := ApplyDataPackage(ctx, cli, pkg3, "test:conn"); err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	var order []string
	for _, r := range *rec {
		if strings.HasPrefix(r.stmt, "SELECT") {
			order = append(order, r.stmt)
		}
	}
	if len(order) != 2 || order[0] != "SELECT 'a'" || order[1] != "SELECT 'b'" {
		t.Errorf("多执行键顺序非确定性: %v (期望字典序 a,b)", order)
	}
}

// ---------------------------------------------------------------------------
// 辅助：行数与标量查询
// ---------------------------------------------------------------------------

func countRows(t *testing.T, cli *cydb.DBCli, tbl string) int {
	t.Helper()
	n, err := cli.Count(tbl, nil)
	if err != nil {
		t.Fatalf("count 失败: %v", err)
	}
	return int(n)
}

func queryScalar(t *testing.T, cli *cydb.DBCli, sql string) string {
	t.Helper()
	rows, err := cli.Query(sql)
	if err != nil {
		t.Fatalf("查询失败 [%s]: %v", sql, err)
	}
	if len(rows) == 0 {
		t.Fatalf("查询无结果 [%s]", sql)
	}
	for _, v := range rows[0] {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// TestExportImportRoundTripMySQL 三轮修复回归：FormatJSON 导出→导入往返
// ①导出条目携带 PK（修复 #1 critical：缺失时导入侧全部数据跳过）
// ②同表同类型条目合并（修复 #2 major：单键索引下建表+数据导致每行独立成条）
// ③往返数据完整 + 回滚产物可回放
func TestExportImportRoundTripMySQL(t *testing.T) {
	cli := newTestMySQLDB(t)
	db := cli.Database()
	tbl := "dp_roundtrip"
	mustCreateTable(t, cli, fmt.Sprintf(testTableDDL, tbl))
	// 预置含引号/反斜杠的特殊行（预置 SQL 走 sqlQuoteLiteral 转义，与被测路径一致）
	for _, row := range []struct{ id, name string }{
		{"rt1", "普通"}, {"it's", "引号"}, {`bs\row`, "反斜杠"},
	} {
		if _, err := cli.Execute(fmt.Sprintf(
			"INSERT INTO `%s` (`id`,`name`,`age`) VALUES (%s,%s,1)",
			tbl, sqlQuoteLiteral("mysql", row.id), sqlQuoteLiteral("mysql", row.name))); err != nil {
			t.Fatalf("预置数据失败: %v", err)
		}
	}

	// 1) 导出为 DataPackage
	filePath := filepath.Join(t.TempDir(), db+".json")
	tr := newTracker(nil, "zh")
	if _, err := exportDatabaseJSON(context.Background(), cli, db, []string{tbl}, filePath, ExportOptions{}, tr); err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取导出文件失败: %v", err)
	}
	pkg, err := LoadDataPackage(data)
	if err != nil {
		t.Fatalf("解析数据包失败: %v", err)
	}

	// 2) 断言：PK 已写入 + 同表同类型条目合并为单条
	var createEntries, dataEntries []*DataEntry
	for _, e := range pkg.Entries {
		if e.Table != tbl {
			continue
		}
		switch e.Type {
		case DataEntryCreateTable:
			createEntries = append(createEntries, e)
		case DataEntryUpsertData:
			dataEntries = append(dataEntries, e)
		}
	}
	if len(createEntries) != 1 {
		t.Fatalf("建表条目数错误: got %d want 1", len(createEntries))
	}
	if len(dataEntries) != 1 {
		t.Fatalf("数据条目未合并（复合键索引失效）: got %d want 1", len(dataEntries))
	}
	if len(dataEntries[0].PK) != 1 || dataEntries[0].PK[0] != "id" {
		t.Fatalf("导出条目缺 PK 字段: %+v", dataEntries[0].PK)
	}
	if len(dataEntries[0].Data) != 3 {
		t.Fatalf("导出行数错误: got %d want 3", len(dataEntries[0].Data))
	}

	// 3) 清空后重新导入：数据完整还原（含特殊字符）
	if _, err := cli.Execute(fmt.Sprintf("DELETE FROM `%s`", tbl)); err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	res, err := ApplyDataPackage(context.Background(), cli, pkg, "test:conn")
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if len(res.SkippedTables) > 0 {
		t.Fatalf("数据被跳过（PK 缺失回归）: %v", res.SkippedTables)
	}
	if n := countRows(t, cli, tbl); n != 3 {
		t.Fatalf("导入后行数错误: got %d want 3", n)
	}
	if got := queryScalar(t, cli, fmt.Sprintf("SELECT `name` FROM `%s` WHERE `id`='it''s'", tbl)); got != "引号" {
		t.Fatalf("含引号 PK 行数据错误: %q", got)
	}

	// 4) 回滚产物回放：恢复到导入前（空表）
	for _, rb := range res.RollbackSQL {
		t.Logf("回滚产物: %s", rb)
	}
	for _, rb := range res.RollbackSQL {
		if _, err := cli.Execute(rb); err != nil {
			t.Fatalf("回滚回放失败 [%s]: %v", rb, err)
		}
	}
	if n := countRows(t, cli, tbl); n != 0 {
		t.Fatalf("回滚回放后行数错误: got %d want 0", n)
	}
}
