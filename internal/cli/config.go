package cli

// 点导入：CLI 层大量复用 service 包的模型别名与入口（NewService/选项模型/错误码）
import (
	"dqex/internal/engine"
	. "dqex/internal/service"
	"os"
	"path/filepath"
	"strings"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/def"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
	"gopkg.in/yaml.v3"
)

// CLI 独立配置文件格式。
// 设计目标：CLI 是 Web 不可用场景下的独立操作闭环，配置文件面向人工编写，
// 采用扁平 snake_case 结构，一个文件只描述一个操作；连接可内联书写，
// 也可通过 *_ref 引用 conn add 保存的连接（ID 或名称）。

// cliConnConfig 配置中的连接段；可引用已保存连接或内联书写
type cliConnConfig struct {
	Ref      string `yaml:"ref"` // 已保存连接（conn add 保存的 ID 或名称），与内联字段二选一
	Type     string `yaml:"type"`
	SubType  string `yaml:"subtype,omitempty"`
	Host     string `yaml:"host,omitempty"`
	Port     int    `yaml:"port,omitempty"`
	User     string `yaml:"user,omitempty"`
	Password string `yaml:"password,omitempty"`
	Database string `yaml:"database,omitempty"`
}

func (c *cliConnConfig) toConn() *DBConnInfo {
	if c == nil || c.Type == "" {
		return nil
	}
	return &DBConnInfo{DBConnection: def.DBConnection{
		Type:    c.Type,
		SubType: c.SubType,
		Host:    c.Host,
		Port:    c.Port,
		Un:      c.User,
		Pw:      c.Password,
		DBName:  c.Database,
	}}
}

type compareConfig struct {
	Source        *cliConnConfig         `yaml:"source"`
	Target        *cliConnConfig         `yaml:"target"`
	SourceRef     string                 `yaml:"source_ref"` // 已保存连接（与 source 二选一）
	TargetRef     string                 `yaml:"target_ref"`
	SourceDB      string                 `yaml:"source_database"` // 覆盖源库名（连接未配库时必填）
	TargetDB      string                 `yaml:"target_database"`
	Databases     []engine.CompareDBPair `yaml:"databases"`      // 多库对比：库对（源库↔目标库）
	DBMap         map[string]string      `yaml:"db_map"`         // 多库对比：源库名→目标库名映射
	Tables        []string               `yaml:"tables"`         // 空=全部表
	Aliases       map[string]string      `yaml:"aliases"`        // 源表: 目标表
	Scope         string                 `yaml:"scope"`          // both|structure|data，默认 both
	Threshold     int                    `yaml:"threshold"`      // 逐行对比阈值，默认 1000
	IgnoreColumns []string               `yaml:"ignore_columns"` // 全局忽略列：所有表数据内容对比时跳过（如 created_at/updated_at）
	// TableIgnoreColumns 表级忽略列（源表: 列列表），与全局忽略列合并生效
	TableIgnoreColumns map[string][]string `yaml:"ignore_columns_by_table"`
	ForceData          bool                `yaml:"force_data"` // 结构不一致时仍强制对比数据（默认跳过）
	Output             string              `yaml:"output"`     // 报告 JSON 保存路径（可选）
}

type exportConfig struct {
	Source     *cliConnConfig `yaml:"source"`
	SourceRef  string         `yaml:"source_ref"`
	Output     string         `yaml:"output"` // 输出目录或 .zip 文件路径
	Name       string         `yaml:"name"`   // 任务名（用于生成文件名）
	Databases  []string       `yaml:"databases"`
	Tables     []string       `yaml:"tables"`
	Conditions []string       `yaml:"conditions"` // "表:完整SELECT"
	SchemaOnly bool           `yaml:"schema_only"`
	DataOnly   bool           `yaml:"data_only"`
	Compress   *bool          `yaml:"compress"` // 默认 true
	Gzip       bool           `yaml:"gzip"`     // SQL 文件 gzip 压缩为 .sql.gz
	// SingleTransaction 一致性快照导出，指针区分未配置（默认 true）与显式 false
	SingleTransaction *bool `yaml:"single_transaction"`
	BatchSize         int   `yaml:"batch_size"`
}

type importConfig struct {
	Target    *cliConnConfig `yaml:"target"`
	TargetRef string         `yaml:"target_ref"`
	Input     string         `yaml:"input"`  // .sql / .sql.gz / .zip
	Reset     string         `yaml:"reset"`  // ""|truncate|drop
	Backup    *bool          `yaml:"backup"` // 默认 true
	BatchSize int            `yaml:"batch_size"`
}

type dictionaryConfig struct {
	Source    *cliConnConfig `yaml:"source"`
	SourceRef string         `yaml:"source_ref"`
	Output    string         `yaml:"output"` // 输出目录或 .zip 文件路径
	Name      string         `yaml:"name"`   // 任务名（用于生成文件名）
	Databases []string       `yaml:"databases"`
	Tables    []string       `yaml:"tables"`
	Compress  *bool          `yaml:"compress"` // 默认 true
}

type migrateConfig struct {
	Source     *cliConnConfig `yaml:"source"`
	Target     *cliConnConfig `yaml:"target"`
	SourceRef  string         `yaml:"source_ref"`
	TargetRef  string         `yaml:"target_ref"`
	SourceDB   string         `yaml:"source_database"` // 覆盖源库名（连接未配库时必填）
	TargetDB   string         `yaml:"target_database"`
	Tables     []string       `yaml:"tables"`
	Conditions []string       `yaml:"conditions"` // "表:完整SELECT"
	SchemaOnly bool           `yaml:"schema_only"`
	DataOnly   bool           `yaml:"data_only"`
	Reset      string         `yaml:"reset"` // ""|truncate|drop
	Backup     *bool          `yaml:"backup"`
	BatchSize  int            `yaml:"batch_size"`
}

// 旧格式（嵌套 camelCase，Web 任务配置导出风格）兼容读取
type legacyConfigFile struct {
	Export  *ExportOptions  `yaml:"export"`
	Import  *ImportOptions  `yaml:"import"`
	Migrate *MigrateOptions `yaml:"migrate"`
	Compare *CompareOptions `yaml:"compare"`
}

func readConfigFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, cygin.WrapError(err, cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf(cliTextsFor(cliLang()).errCfgRead, path))
	}
	return data, nil
}

func parseYAML[T any](data []byte, path string, zero T) (*T, error) {
	cfg := zero
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, cygin.WrapError(err, cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf(cliTextsFor(cliLang()).errCfgParse, path))
	}
	return &cfg, nil
}

// yamlHasKeys 顶层是否包含任一指定键（用于新旧格式探测）
func yamlHasKeys(data []byte, keys ...string) bool {
	m := map[string]any{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return false
	}
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

// loadCompareConfig 解析对比配置；新格式优先，失败时回退旧嵌套格式
func loadCompareConfig(path string) (*compareConfig, error) {
	data, err := readConfigFile(path)
	if err != nil {
		return nil, err
	}
	// camelCase 标记键：优先按旧嵌套格式解析
	if yamlHasKeys(data, "sourceConn", "targetConn", "structureOnly", "dataOnly") {
		if legacy, lerr := parseYAML(data, path, legacyConfigFile{}); lerr == nil && legacy.Compare != nil {
			return compareFromLegacy(legacy.Compare), nil
		}
	}
	cfg, err := parseYAML(data, path, compareConfig{})
	if err == nil && (cfg.Source != nil || cfg.Target != nil || cfg.SourceRef != "" || cfg.TargetRef != "") {
		return cfg, nil
	}
	if legacy, lerr := parseYAML(data, path, legacyConfigFile{}); lerr == nil && legacy.Compare != nil {
		return compareFromLegacy(legacy.Compare), nil
	}
	if err != nil {
		return nil, err
	}
	return nil, cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf(cliTextsFor(cliLang()).errCfgNoCmp, path))
}

func loadExportConfig(path string) (*exportConfig, error) {
	data, err := readConfigFile(path)
	if err != nil {
		return nil, err
	}
	if yamlHasKeys(data, "sourceConn", "outputDir", "schemaOnly", "dataOnly") {
		if legacy, lerr := parseYAML(data, path, legacyConfigFile{}); lerr == nil && legacy.Export != nil {
			return exportFromLegacy(legacy.Export), nil
		}
	}
	cfg, err := parseYAML(data, path, exportConfig{})
	if err == nil && (cfg.Source != nil || cfg.SourceRef != "") {
		return cfg, nil
	}
	if legacy, lerr := parseYAML(data, path, legacyConfigFile{}); lerr == nil && legacy.Export != nil {
		return exportFromLegacy(legacy.Export), nil
	}
	if err != nil {
		return nil, err
	}
	return nil, cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf(cliTextsFor(cliLang()).errCfgNoExp, path))
}

func loadDictionaryConfig(path string) (*dictionaryConfig, error) {
	data, err := readConfigFile(path)
	if err != nil {
		return nil, err
	}
	cfg, err := parseYAML(data, path, dictionaryConfig{})
	if err != nil {
		return nil, err
	}
	if cfg.Source != nil || cfg.SourceRef != "" {
		return cfg, nil
	}
	return nil, cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf(cliTextsFor(cliLang()).errCfgNoDict, path))
}

func loadImportConfig(path string) (*importConfig, error) {
	data, err := readConfigFile(path)
	if err != nil {
		return nil, err
	}
	if yamlHasKeys(data, "targetConn", "inputPath", "resetMode") {
		if legacy, lerr := parseYAML(data, path, legacyConfigFile{}); lerr == nil && legacy.Import != nil {
			return importFromLegacy(legacy.Import), nil
		}
	}
	cfg, err := parseYAML(data, path, importConfig{})
	if err == nil && (cfg.Target != nil || cfg.TargetRef != "") {
		return cfg, nil
	}
	if legacy, lerr := parseYAML(data, path, legacyConfigFile{}); lerr == nil && legacy.Import != nil {
		return importFromLegacy(legacy.Import), nil
	}
	if err != nil {
		return nil, err
	}
	return nil, cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf(cliTextsFor(cliLang()).errCfgNoImp, path))
}

func loadMigrateConfig(path string) (*migrateConfig, error) {
	data, err := readConfigFile(path)
	if err != nil {
		return nil, err
	}
	if yamlHasKeys(data, "sourceConn", "targetConn", "schemaOnly", "dataOnly") {
		if legacy, lerr := parseYAML(data, path, legacyConfigFile{}); lerr == nil && legacy.Migrate != nil {
			return migrateFromLegacy(legacy.Migrate), nil
		}
	}
	cfg, err := parseYAML(data, path, migrateConfig{})
	if err == nil && (cfg.Source != nil || cfg.Target != nil || cfg.SourceRef != "" || cfg.TargetRef != "") {
		return cfg, nil
	}
	if legacy, lerr := parseYAML(data, path, legacyConfigFile{}); lerr == nil && legacy.Migrate != nil {
		return migrateFromLegacy(legacy.Migrate), nil
	}
	if err != nil {
		return nil, err
	}
	return nil, cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf(cliTextsFor(cliLang()).errCfgNoMig, path))
}

// ---- 旧格式转换（仅读取兼容） ----

func connToCli(c *DBConnInfo) *cliConnConfig {
	if c == nil {
		return nil
	}
	return &cliConnConfig{Type: c.Type, SubType: c.SubType, Host: c.Host, Port: c.Port, User: c.Un, Password: c.Pw, Database: c.DBName}
}

func compareFromLegacy(o *CompareOptions) *compareConfig {
	cfg := &compareConfig{
		Source:        connToCli(o.Source),
		Target:        connToCli(o.Target),
		SourceRef:     o.SourceConn,
		TargetRef:     o.TargetConn,
		Tables:        o.Tables,
		Threshold:     o.Threshold,
		IgnoreColumns: o.IgnoreColumns,
		ForceData:     o.ForceData,
		Scope:         "both",
	}
	if len(o.Aliases) > 0 {
		cfg.Aliases = make(map[string]string, len(o.Aliases))
		for _, a := range o.Aliases {
			if a.Target != "" {
				cfg.Aliases[a.Source] = a.Target
			}
			if len(a.IgnoreColumns) > 0 {
				if cfg.TableIgnoreColumns == nil {
					cfg.TableIgnoreColumns = make(map[string][]string)
				}
				cfg.TableIgnoreColumns[a.Source] = a.IgnoreColumns
			}
		}
	}
	switch {
	case o.StructureOnly:
		cfg.Scope = "structure"
	case o.DataOnly:
		cfg.Scope = "data"
	}
	return cfg
}

func exportFromLegacy(o *ExportOptions) *exportConfig {
	conds := make([]string, 0, len(o.Conditions))
	for _, c := range o.Conditions {
		if c.Query != "" {
			conds = append(conds, c.TableName+":"+c.Query)
		} else if c.Where != "" {
			conds = append(conds, c.TableName+":"+c.Where)
		}
	}
	compress := o.Compress
	singleTx := o.SingleTransaction
	return &exportConfig{
		Source: connToCli(o.Source), SourceRef: o.SourceConn,
		Output: o.OutputDir, Name: o.TaskName,
		Databases: o.Databases, Tables: o.Tables,
		Conditions: conds, SchemaOnly: o.SchemaOnly, DataOnly: o.DataOnly,
		Compress: &compress, Gzip: o.Gzip, SingleTransaction: &singleTx, BatchSize: o.BatchSize,
	}
}

func importFromLegacy(o *ImportOptions) *importConfig {
	backup := o.Backup
	return &importConfig{
		Target: connToCli(o.Target), TargetRef: o.TargetConn,
		Input: o.InputPath, Reset: string(o.ResetMode),
		Backup: &backup, BatchSize: o.BatchSize,
	}
}

func migrateFromLegacy(o *MigrateOptions) *migrateConfig {
	conds := make([]string, 0, len(o.Conditions))
	for _, c := range o.Conditions {
		if c.Query != "" {
			conds = append(conds, c.TableName+":"+c.Query)
		} else if c.Where != "" {
			conds = append(conds, c.TableName+":"+c.Where)
		}
	}
	backup := o.Backup
	return &migrateConfig{
		Source: connToCli(o.Source), Target: connToCli(o.Target),
		SourceRef: o.SourceConn, TargetRef: o.TargetConn,
		Tables: o.Tables, Conditions: conds,
		SchemaOnly: o.SchemaOnly, DataOnly: o.DataOnly,
		Reset: string(o.ResetMode), Backup: &backup, BatchSize: o.BatchSize,
	}
}

// ---- 配置模板（--gen-config 输出，人工填写起点） ----

const tplConn = `# 内联连接配置；也可删除本段改用 source_ref: 已保存连接名（dqex conn add 保存）
type: mysql            # mysql / postgresql / oracle
subtype: ""            # 可选：兼容数据库产品（如 oceanbase/mariadb、gaussdb/kingbase、dameng），留空=原生
host: 127.0.0.1
port: 3306
user: root
password: ""
database: ""           # 留空=由 --databases/库内全部库决定
`

var configTemplates = map[string]string{
	"compare": `# dqex compare 独立配置文件（dqex compare --config compare.yaml）
source:
` + indentBlock(tplConn, "  ") + `target:
` + indentBlock(tplConn, "  ") + `# tables:              # 指定对比的表，留空=全部
#   - table_a
# source_database: db1 # 覆盖源库名（source_ref 未配库时必填）
# target_database: db1
# aliases:             # 不同名但需对比的表配对（源表: 目标表）
#   old_table: new_table
scope: both            # both | structure | data
threshold: 1000        # 单表行数超过阈值时仅对比行数，不逐行比对
# ignore_columns:      # 全局忽略列：所有表数据对比时跳过（如时间戳列，可选）
#   - created_at
#   - updated_at
# ignore_columns_by_table:   # 表级忽略列：仅对指定表生效，与全局合并（可选）
#   t_order: [sync_flag]
# force_data: false    # 结构不一致时仍强制对比数据（默认跳过）
# output: report.json  # 对比报告 JSON 保存路径（可选）
`,
	"export": `# dqex export 独立配置文件（dqex export --config export.yaml）
source:
` + indentBlock(tplConn, "  ") + `# output: ./export.zip  # 输出 .zip 文件路径或目录（留空=默认导出目录）
# name: myexport        # 任务名（用于生成文件名）
# databases:            # 指定库，留空=连接配置的库
#   - db1
# tables:               # 指定表，留空=全部
#   - table_a
# conditions:           # 表级过滤条件 "表名:完整SELECT"
#   - "table_a: SELECT * FROM table_a WHERE status = 1"
schema_only: false     # 仅导出结构
data_only: false       # 仅导出数据
compress: true         # 打包为 zip
gzip: false            # SQL 文件 gzip 压缩为 .sql.gz（导入侧自动解压）
single_transaction: true  # 一致性快照导出（仅 MySQL/PostgreSQL 生效）
batch_size: 500
`,
	"import": `# dqex import 独立配置文件（dqex import --config import.yaml）
target:
` + indentBlock(tplConn, "  ") + `input: ./export.zip    # 导入文件（.sql / .sql.gz / .zip）
reset: ""              # "" 直接追加 | truncate 清空表 | drop 删除重建
backup: true           # 重置前创建备份表（仅 reset 非空时生效）
batch_size: 500
`,
	"migrate": `# dqex migrate 独立配置文件（dqex migrate --config migrate.yaml）
source:
` + indentBlock(tplConn, "  ") + `target:
` + indentBlock(tplConn, "  ") + `# tables:              # 指定表，留空=全部
#   - table_a
# conditions:          # 表级过滤条件 "表名:完整SELECT"
#   - "table_a: SELECT * FROM table_a WHERE status = 1"
schema_only: false
data_only: false
reset: ""              # "" 直接追加 | truncate | drop
backup: true
batch_size: 500
`,
	"dictionary": `# dqex dictionary 独立配置文件（dqex dictionary --config dictionary.yaml）
source:
` + indentBlock(tplConn, "  ") + `# output: ./dictionary.zip  # 输出 .zip 文件路径或目录（留空=默认导出目录）
# name: mydict              # 任务名（用于生成文件名）
# databases:                # 指定库，留空=连接配置的库
#   - db1
# tables:                   # 指定表，留空=全部
#   - table_a
compress: true              # 打包为 zip
`,
}

// indentBlock 给模板非空行加缩进前缀
func indentBlock(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

// printConfigTemplate 输出生成模板并返回 true（调用方直接结束命令）
func printConfigTemplate(kind string) bool {
	tpl, ok := configTemplates[kind]
	if !ok {
		return false
	}
	os.Stdout.WriteString(tpl)
	return true
}

// ---- 配置 → 引擎选项 ----

func validResetMode(v string) (ResetMode, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "none":
		return ResetNone, nil
	case "truncate":
		return ResetTruncate, nil
	case "drop":
		return ResetDrop, nil
	}
	return ResetNone, cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf(cliTextsFor(cliLang()).errResetMode, v))
}

// compareOptsFromConfig 配置转引擎选项；requireConns=true 时校验连接必须存在（task save 用），
// false 时允许空配置（命令后续用 flags 补齐）
func compareOptsFromConfig(cfg *compareConfig, requireConns bool) (CompareOptions, string, error) {
	opts := CompareOptions{Lang: cliLang()}
	if cfg.Source != nil {
		opts.Source = cfg.Source.toConn()
	}
	opts.SourceConn = cfg.SourceRef
	if cfg.Target != nil {
		opts.Target = cfg.Target.toConn()
	}
	opts.TargetConn = cfg.TargetRef
	// 库名覆盖：内联连接直接写入，引用连接由 service 层解析后补齐
	if opts.Source != nil && cfg.SourceDB != "" {
		opts.Source.DBName = cfg.SourceDB
	}
	if opts.Target != nil && cfg.TargetDB != "" {
		opts.Target.DBName = cfg.TargetDB
	}
	if requireConns && opts.Source == nil && opts.SourceConn == "" {
		return opts, "", cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetails(cliTextsFor(cliLang()).errNoSrcConn))
	}
	if requireConns && opts.Target == nil && opts.TargetConn == "" {
		return opts, "", cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetails(cliTextsFor(cliLang()).errNoTgtConn))
	}
	opts.Tables = cfg.Tables
	opts.IgnoreColumns = cfg.IgnoreColumns
	opts.ForceData = cfg.ForceData
	for src, tgt := range cfg.Aliases {
		opts.Aliases = append(opts.Aliases, TableAlias{Source: src, Target: tgt})
	}
	// 表级忽略列：并入同名 alias 条目，无则新建（Target 空=同名匹配）
	for src, cols := range cfg.TableIgnoreColumns {
		merged := false
		for i := range opts.Aliases {
			if strings.EqualFold(strings.TrimSpace(opts.Aliases[i].Source), strings.TrimSpace(src)) {
				opts.Aliases[i].IgnoreColumns = cols
				merged = true
				break
			}
		}
		if !merged {
			opts.Aliases = append(opts.Aliases, TableAlias{Source: src, IgnoreColumns: cols})
		}
	}
	switch scope := strings.ToLower(strings.TrimSpace(cfg.Scope)); scope {
	case "", "both":
	case "structure":
		opts.StructureOnly = true
	case "data":
		opts.DataOnly = true
	default:
		return opts, "", cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf(cliTextsFor(cliLang()).errCmpScope, scope))
	}
	if cfg.Threshold < 0 {
		return opts, "", cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetails(cliTextsFor(cliLang()).errThresholdNeg))
	}
	opts.Threshold = cfg.Threshold
	return opts, cfg.Output, nil
}

// splitOutput 输出路径拆分：.zip 结尾返回目标文件路径，否则视为目录（均转绝对路径）
func splitOutput(v string) (outputDir, wantZip string) {
	if v == "" {
		return "", ""
	}
	abs, _ := filepath.Abs(v)
	if strings.HasSuffix(strings.ToLower(abs), ".zip") {
		return filepath.Dir(abs), abs
	}
	return abs, ""
}

func exportOptsFromConfig(cfg *exportConfig) (ExportOptions, error) {
	opts := ExportOptions{Compress: true, SingleTransaction: true, Lang: cliLang()}
	if cfg.Source != nil {
		opts.Source = cfg.Source.toConn()
	}
	opts.SourceConn = cfg.SourceRef
	if opts.Source == nil && opts.SourceConn == "" {
		return opts, cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetails(cliTextsFor(cliLang()).errNoSrcConn))
	}
	opts.OutputDir, _ = splitOutput(cfg.Output)
	opts.TaskName = cfg.Name
	opts.Databases = cfg.Databases
	opts.Tables = cfg.Tables
	opts.Conditions = parseTableConds(cfg.Conditions)
	opts.SchemaOnly = cfg.SchemaOnly
	opts.DataOnly = cfg.DataOnly
	if cfg.Compress != nil {
		opts.Compress = *cfg.Compress
	}
	opts.Gzip = cfg.Gzip
	if cfg.SingleTransaction != nil {
		opts.SingleTransaction = *cfg.SingleTransaction
	}
	opts.BatchSize = cfg.BatchSize
	return opts, nil
}

func dictionaryOptsFromConfig(cfg *dictionaryConfig) (DictionaryOptions, error) {
	opts := DictionaryOptions{Compress: true, Lang: cliLang()}
	if cfg.Source != nil {
		opts.Source = cfg.Source.toConn()
	}
	opts.SourceConn = cfg.SourceRef
	if opts.Source == nil && opts.SourceConn == "" {
		return opts, cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetails(cliTextsFor(cliLang()).errNoSrcConn))
	}
	opts.OutputDir, _ = splitOutput(cfg.Output)
	opts.TaskName = cfg.Name
	opts.Databases = cfg.Databases
	opts.Tables = cfg.Tables
	if cfg.Compress != nil {
		opts.Compress = *cfg.Compress
	}
	return opts, nil
}

func importOptsFromConfig(cfg *importConfig) (ImportOptions, error) {
	opts := ImportOptions{Backup: true, Lang: cliLang()}
	if cfg.Target != nil {
		opts.Target = cfg.Target.toConn()
	}
	opts.TargetConn = cfg.TargetRef
	if opts.Target == nil && opts.TargetConn == "" {
		return opts, cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetails(cliTextsFor(cliLang()).errNoTgtConn))
	}
	opts.InputPath = cfg.Input
	reset, err := validResetMode(cfg.Reset)
	if err != nil {
		return opts, err
	}
	opts.ResetMode = reset
	if cfg.Backup != nil {
		opts.Backup = *cfg.Backup
	}
	opts.BatchSize = cfg.BatchSize
	return opts, nil
}

func migrateOptsFromConfig(cfg *migrateConfig) (MigrateOptions, error) {
	opts := MigrateOptions{Backup: true, Lang: cliLang()}
	if cfg.Source != nil {
		opts.Source = cfg.Source.toConn()
	}
	opts.SourceConn = cfg.SourceRef
	if cfg.Target != nil {
		opts.Target = cfg.Target.toConn()
	}
	opts.TargetConn = cfg.TargetRef
	// 库名覆盖：内联连接直接写入，引用连接由命令层解析后补齐
	if opts.Source != nil && cfg.SourceDB != "" {
		opts.Source.DBName = cfg.SourceDB
	}
	if opts.Target != nil && cfg.TargetDB != "" {
		opts.Target.DBName = cfg.TargetDB
	}
	if opts.Source == nil && opts.SourceConn == "" {
		return opts, cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetails(cliTextsFor(cliLang()).errNoSrcConn))
	}
	if opts.Target == nil && opts.TargetConn == "" {
		return opts, cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetails(cliTextsFor(cliLang()).errNoTgtConn))
	}
	opts.Tables = cfg.Tables
	opts.Conditions = parseTableConds(cfg.Conditions)
	opts.SchemaOnly = cfg.SchemaOnly
	opts.DataOnly = cfg.DataOnly
	reset, err := validResetMode(cfg.Reset)
	if err != nil {
		return opts, err
	}
	opts.ResetMode = reset
	if cfg.Backup != nil {
		opts.Backup = *cfg.Backup
	}
	opts.BatchSize = cfg.BatchSize
	return opts, nil
}

// detectConfigKind 推断配置文件所属操作类型（供 task save 自动识别）；hint 非空时直接使用
func detectConfigKind(data []byte, hint string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(hint)) {
	case "":
	case "export", "import", "migrate", "compare", "dictionary":
		return strings.ToLower(strings.TrimSpace(hint)), nil
	default:
		return "", cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetails(sprintf(cliTextsFor(cliLang()).errTaskHint, hint)))
	}
	m := map[string]any{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return "", cygin.WrapError(err, cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetails(cliTextsFor(cliLang()).errCfgParseOnly))
	}
	has := func(keys ...string) bool {
		for _, k := range keys {
			if _, ok := m[k]; ok {
				return true
			}
		}
		return false
	}
	// 旧嵌套格式
	for _, k := range []string{"export", "import", "migrate", "compare"} {
		if _, ok := m[k]; ok {
			return k, nil
		}
	}
	switch {
	case has("input"):
		return "import", nil
	case has("scope", "threshold", "aliases"):
		return "compare", nil
	case has("source", "target"):
		return "migrate", nil
	case has("source"):
		return "export", nil
	case has("target"):
		return "import", nil
	}
	return "", cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetails(cliTextsFor(cliLang()).errCfgTypeUnknown))
}
