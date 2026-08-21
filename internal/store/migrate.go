package store

import (
	"github.com/fj1981/infrakit/pkg/cydb/def"
	"github.com/fj1981/infrakit/pkg/cydb/migrate"
)

// migrateModels 对给定数据库客户端执行全部模型的自动迁移。
// 新增表只需在 allModels 追加结构体，无需修改本函数。
func migrateModels(cli def.DatabaseClient) error {
	for _, m := range allModels {
		if err := migrate.AutoMigrate(cli, m); err != nil {
			return err
		}
	}
	return nil
}
