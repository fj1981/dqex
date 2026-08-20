package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"dbimpex/internal/engine"
	. "dbimpex/internal/service"

	"github.com/spf13/cobra"
)

var snapConn connFlags

var snapshotCmd = &cobra.Command{
	Use:     "snapshot",
	Aliases: []string{"snap"},
	Short:   "管理数据库快照：创建、列表、查看、删除、对比",
	Long: `管理数据库快照，支持与当前数据库状态对比。

独立闭环用法：
  dbx snapshot create -c <连接名> -d <数据库> -n <名称>     # 创建快照
  dbx snapshot list                                           # 列出所有快照
  dbx snapshot show -i <快照ID>                               # 查看快照详情
  dbx snapshot delete -i <快照ID>                             # 删除快照
  dbx snapshot compare -i <快照ID> -c <连接名> -d <数据库>    # 快照 vs 当前库`,
}

var (
	snapCreateCmd = &cobra.Command{
		Use:   "create",
		Short: "创建数据库快照",
		Long:  "连接数据库并创建快照（记录所有表结构 + 行数统计）",
		RunE:  cliSnapshotCreate,
	}
	snapListCmd = &cobra.Command{
		Use:   "list",
		Short: "列出所有快照",
		RunE:  cliSnapshotList,
	}
	snapShowCmd = &cobra.Command{
		Use:   "show",
		Short: "查看快照详情",
		RunE:  cliSnapshotShow,
	}
	snapDeleteCmd = &cobra.Command{
		Use:   "delete",
		Short: "删除快照",
		RunE:  cliSnapshotDelete,
	}
	snapCompareCmd = &cobra.Command{
		Use:   "compare",
		Short: "快照与当前数据库对比",
		Long:  "将快照的表结构与当前数据库进行对比，输出差异报告",
		RunE:  cliSnapshotCompare,
	}
)

var (
	snapCreateName        string
	snapCreateDesc        string
	snapCreateConn        string
	snapCreateDBs         string
	snapCreateSamples     bool
	snapCreateSampleLimit int
	snapShowID            string
	snapDeleteID          string
	snapCompareID         string
	snapCompareTargetConn string
	snapCompareDB         string
	snapCompareDBMap      string
	snapCompareOutput     string
)

func init() {
	// create flags
	fc := snapCreateCmd.Flags()
	fc.StringVarP(&snapCreateConn, "conn", "c", "", "已保存连接名（ID 或名称）")
	fc.StringVarP(&snapCreateDBs, "database", "d", "", "数据库名，逗号分隔支持多库（留空=使用连接默认库）")
	fc.StringVarP(&snapCreateName, "name", "n", "", "快照名称")
	fc.StringVar(&snapCreateDesc, "desc", "", "备注说明")
	fc.BoolVar(&snapCreateSamples, "samples", false, "保存前 N 行数据采样")
	fc.IntVar(&snapCreateSampleLimit, "sample-limit", 0, "每表采样行数（<=0 用默认 10，仅在 --samples 开启时生效）")
	_ = snapCreateCmd.MarkFlagRequired("conn")
	_ = snapCreateCmd.MarkFlagRequired("name")
	_ = snapCreateCmd.RegisterFlagCompletionFunc("conn", completeConnNames)

	// show/delete flags
	snapShowCmd.Flags().StringVarP(&snapShowID, "id", "i", "", "快照 ID")
	_ = snapShowCmd.MarkFlagRequired("id")
	snapDeleteCmd.Flags().StringVarP(&snapDeleteID, "id", "i", "", "快照 ID")
	_ = snapDeleteCmd.MarkFlagRequired("id")

	// compare flags
	fcmp := snapCompareCmd.Flags()
	fcmp.StringVarP(&snapCompareID, "id", "i", "", "快照 ID")
	fcmp.StringVarP(&snapCompareTargetConn, "target-conn", "c", "", "目标连接名（ID 或名称）")
	fcmp.StringVarP(&snapCompareDB, "database", "d", "", "目标数据库名（覆盖目标连接默认库）")
	fcmp.StringVar(&snapCompareDBMap, "db-map", "", "快照库→目标库映射，逗号分隔的 源库=目标库 对（如 db1=db2,db3=db4）")
	fcmp.StringVar(&snapCompareOutput, "output", "", "对比报告 JSON 额外保存路径")
	_ = snapCompareCmd.MarkFlagRequired("id")
	_ = snapCompareCmd.MarkFlagRequired("target-conn")
	_ = snapCompareCmd.RegisterFlagCompletionFunc("target-conn", completeConnNames)

	snapshotCmd.AddCommand(snapCreateCmd, snapListCmd, snapShowCmd, snapDeleteCmd, snapCompareCmd)
	rootCmd.AddCommand(snapshotCmd)
}

func cliSnapshotCreate(cmd *cobra.Command, args []string) error {
	svc, err := newCliService()
	if err != nil {
		return err
	}
	cb, _ := cliProgress()
	ctx := context.Background()

	dbNames := splitCSV(snapCreateDBs)
	snap, err := svc.CreateSnapshot(ctx, snapCreateConn, dbNames, snapCreateName, snapCreateDesc, snapCreateSamples, snapCreateSampleLimit, cliLang(), cb)
	if err != nil {
		return err
	}

	dbLabel := snap.DBName
	if len(snap.Databases) > 0 {
		names := make([]string, 0, len(snap.Databases))
		for _, d := range snap.Databases {
			names = append(names, d.DBName)
		}
		dbLabel = strings.Join(names, ", ")
	}
	txt := cliTextsFor(cliLang())
	printf(txt.snapCreated+"\n", green("✓"))
	printf(txt.snapID+"\n", dim(snap.ID))
	printf(txt.snapName+"\n", snap.Name)
	printf(txt.snapDBs+"\n", dbLabel, snap.DBType)
	printf(txt.snapTables+"\n", snap.TableCount)
	printf(txt.snapRows+"\n", humanRows(snap.TotalRows))
	if snap.Description != "" {
		printf(txt.snapDesc+"\n", snap.Description)
	}
	return nil
}

func cliSnapshotList(cmd *cobra.Command, args []string) error {
	svc, err := newCliService()
	if err != nil {
		return err
	}
	infos := svc.ListSnapshots()
	if len(infos) == 0 {
		fmt.Println(cliTextsFor(cliLang()).snapNone)
		return nil
	}
	txt := cliTextsFor(cliLang())
	printf(txt.snapListTitle+"\n\n", bold(txt.snapListWord), len(infos))
	for _, info := range infos {
		dbs := info.DBName
		if len(info.DBNames) > 0 {
			dbs = strings.Join(info.DBNames, ", ")
		}
		printf("  %s  %s\n", bold(info.ID), green(info.Name))
		printf("     %s | %s | %s | %s\n",
			dim(dbs), dim(info.DBType),
			sprintf(txt.snapTableUnit, info.TableCount, humanRows(info.TotalRows)),
			time.Unix(info.CreatedAt, 0).Format("2006-01-02 15:04"))
		if info.Description != "" {
			printf("     %s: %s\n", dim(txt.snapDesc), dim(info.Description))
		}
	}
	return nil
}

func cliSnapshotShow(cmd *cobra.Command, args []string) error {
	svc, err := newCliService()
	if err != nil {
		return err
	}
	snap, err := svc.GetSnapshot(snapShowID)
	if err != nil {
		return err
	}
	printf("%s %s\n", bold(cliTextsFor(cliLang()).snapDetailTitle), green(snap.Name))
	printf("%s\n", dim(snap.ID))
	txt := cliTextsFor(cliLang())
	printf(txt.snapConn+"\n", snap.ConnLabel)
	dbs := snap.Databases
	if len(dbs) == 0 && snap.DBName != "" {
		dbs = []engine.SnapshotDatabase{{DBName: snap.DBName, TableCount: snap.TableCount, TotalRows: snap.TotalRows, Tables: snap.Tables}}
	}
	dbLabel := ""
	for _, d := range dbs {
		if dbLabel != "" {
			dbLabel += ", "
		}
		dbLabel += d.DBName
	}
	printf(txt.snapDBs+"\n", dbLabel, snap.DBType)
	printf(txt.snapCreatedAt+"\n", time.Unix(snap.CreatedAt, 0).Format("2006-01-02 15:04:05"))
	printf(txt.snapTables+"\n", snap.TableCount)
	printf(txt.snapRows+"\n", humanRows(snap.TotalRows))
	if snap.Description != "" {
		printf(txt.snapDesc+"\n", snap.Description)
	}
	for _, d := range dbs {
		printf(txt.snapDBLine+"\n", bold(txt.snapDBWord), bold(d.DBName), d.TableCount, humanRows(d.TotalRows))
		for _, st := range d.Tables {
			pkInfo := ""
			if len(st.PrimaryKey) > 0 {
				pkInfo = sprintf(txt.snapPK, st.PrimaryKey)
			}
			printf(txt.snapTableLine+"\n", st.Name, len(st.Columns), humanRows(st.RowCount), dim(pkInfo))
		}
	}
	return nil
}

func cliSnapshotDelete(cmd *cobra.Command, args []string) error {
	svc, err := newCliService()
	if err != nil {
		return err
	}
	if err := svc.DeleteSnapshot(snapDeleteID); err != nil {
		return err
	}
	printf(cliTextsFor(cliLang()).snapDeleted+"\n", green("✓"), dim(snapDeleteID))
	return nil
}

func cliSnapshotCompare(cmd *cobra.Command, args []string) error {
	svc, err := newCliService()
	if err != nil {
		return err
	}
	snap, err := svc.GetSnapshot(snapCompareID)
	if err != nil {
		return err
	}
	target, err := overrideConnDB(svc, snapCompareTargetConn, snapCompareDB)
	if err != nil {
		return err
	}

	dbMapping := parseDBMap(snapCompareDBMap)
	opts := SnapshotCompareOptions{
		SnapshotID: snapCompareID,
		TargetConn: snapCompareTargetConn,
		Target:     target,
		DBMapping:  dbMapping,
		Lang:       cliLang(),
	}

	cb, _ := cliProgress()
	ctx := context.Background()

	taskID, result, err := svc.RunSnapshotCompareRecorded(ctx, snap, target, opts, cb)
	if err != nil {
		return err
	}

	sm := result.Summary
	printf("\n%s 快照对比完成\n", green("✓"))
	printf("  快照: %s (%s)\n", snap.Name, time.Unix(snap.CreatedAt, 0).Format("2006-01-02 15:04"))
	printf("  对比: %s (当前)\n", snapCompareTargetConn)
	printf("  结果: 共%d项, 一致%d, 结构差异%d, 数据差异%d\n",
		sm.Total, sm.Matched, sm.StructureDiff, sm.DataDiff)

	// 输出差异明细（按库分组）
	printf("\n%s\n", bold("差异明细:"))
	hasDiff := false
	for _, db := range result.Databases {
		printf("\n%s %s ↔ %s\n", bold("库"), bold(db.SourceDB), bold(db.TargetDB))
		dbHasDiff := false
		for _, tr := range db.Tables {
			if tr.Status != "both" {
				hasDiff = true
				dbHasDiff = true
				if tr.Status == "source_only" {
					printf("  %s %s\n", yellow("−"), dim(tr.Name+" (仅快照有)"))
				} else {
					printf("  %s %s\n", yellow("+"), dim(tr.Name+" (仅当前库有)"))
				}
				continue
			}
			if tr.Columns != nil && !tr.Columns.Matched {
				hasDiff = true
				dbHasDiff = true
				printf("  %s %s 结构差异 (+%d -%d ±%d)\n",
					red("✗"), tr.Name,
					len(tr.Columns.SourceOnly), len(tr.Columns.TargetOnly), len(tr.Columns.Different))
			}
			if tr.Data != nil && !tr.Data.Equal && tr.Data.Mode != "skipped" {
				hasDiff = true
				dbHasDiff = true
				printf("  %s %s 数据差异 (快照%d → 当前%d)\n",
					red("✗"), tr.Name, tr.Data.SourceRows, tr.Data.TargetRows)
			}
		}
		if !dbHasDiff {
			printf("  %s 无差异\n", green("✓"))
		}
	}
	if !hasDiff {
		printf("  %s 无差异\n", green("✓"))
	}

	if snapCompareOutput != "" {
		data, _ := json.MarshalIndent(result, "", "  ")
		if err := os.WriteFile(snapCompareOutput, data, 0o644); err != nil {
			return fmt.Errorf("保存对比报告失败: %w", err)
		}
		printf("\n对比报告已保存: %s\n", snapCompareOutput)
	}

	_ = taskID
	return nil
}
