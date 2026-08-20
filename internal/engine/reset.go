package engine

import (
	"fmt"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb"
)

// backupTableName 生成备份表名
func backupTableName(table string) string {
	return BackupTablePrefix + table
}

// dropTableIfExists 删除表（先检查存在性，兼容 Oracle 无 IF EXISTS 语法）
func dropTableIfExists(cli *cydb.DBCli, table string) error {
	exist, err := cli.IsTableExist(table)
	if err != nil {
		return err
	}
	if !exist {
		return nil
	}
	escaped := EscapeTable(cli.DBType(), cli.DBSubType(), table)
	_, err = cli.DirectExecute(fmt.Sprintf("DROP TABLE %s", escaped))
	return err
}

// backupTable 在目标库创建备份表 __dbimpex_bak_{表名}（CREATE TABLE AS SELECT）
func backupTable(cli *cydb.DBCli, table string, t *tracker) error {
	bak := backupTableName(table)
	// 清理上一次遗留的备份表
	if err := dropTableIfExists(cli, bak); err != nil {
		return fmt.Errorf("清理旧备份表失败: %w", err)
	}
	escaped := EscapeTable(cli.DBType(), cli.DBSubType(), table)
	escapedBak := EscapeTable(cli.DBType(), cli.DBSubType(), bak)
	sql := fmt.Sprintf("CREATE TABLE %s AS SELECT * FROM %s", escapedBak, escaped)
	if _, err := cli.DirectExecute(sql); err != nil {
		return fmt.Errorf("创建备份表失败: %w", err)
	}
	t.log(engineTextsFor(t.lang).rstBackup, bak)
	return nil
}

// dropBackupTable 清理备份表（导入/迁移成功后调用）
func dropBackupTable(cli *cydb.DBCli, table string) error {
	return dropTableIfExists(cli, backupTableName(table))
}

// resetTable 对单表执行重置：可选备份 + truncate/drop。
// 返回是否创建了备份表。
func resetTable(cli *cydb.DBCli, table string, mode ResetMode, backup bool, t *tracker) (bool, error) {
	if mode == ResetNone {
		return false, nil
	}
	exist, err := cli.IsTableExist(table)
	if err != nil {
		return false, err
	}
	if !exist {
		return false, nil // 表不存在无需重置
	}
	if backup {
		if err := backupTable(cli, table, t); err != nil {
			return false, err
		}
	}
	switch mode {
	case ResetTruncate:
		escaped := EscapeTable(cli.DBType(), cli.DBSubType(), table)
		if _, err := cli.DirectExecute(fmt.Sprintf("TRUNCATE TABLE %s", escaped)); err != nil {
			return backup, fmt.Errorf("清空表 %s 失败: %w", table, err)
		}
		t.log(engineTextsFor(t.lang).rstTrunc, table)
	case ResetDrop:
		if err := dropTableIfExists(cli, table); err != nil {
			return backup, fmt.Errorf("删除表 %s 失败: %w", table, err)
		}
		t.log(engineTextsFor(t.lang).rstDrop, table)
	}
	return backup, nil
}

func resetDesc(mode ResetMode, lang string) string {
	return engineTextsFor(lang).resetDesc[mode]
}
