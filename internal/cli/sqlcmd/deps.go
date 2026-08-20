package sqlcmd

import (
	"dbimpex/internal/engine"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/def"
)

// ColorFuncs ANSI 颜色函数。
type ColorFuncs struct {
	Green  func(string) string
	Red    func(string) string
	Yellow func(string) string
	Bold   func(string) string
	Dim    func(string) string
}

// ConnResolver 连接解析器（由父包注入，避免循环引用）。
type ConnResolver struct {
	NewCliService func() (any, error)                                               // 返回 *service.Service
	ResolveConn   func(svc any, key, dbOverride string) (*engine.DBConnInfo, error) // 已保存连接解析
	RegisterFlags func(cmd any, prefix string, cf *ConnFlags)                       // 注册连接 flags
	RegisterAlias func(cmd any, aliasPrefix, refPrefix string, cf *ConnFlags)       // 注册别名
}

// ConnFlags 连接 flag 结构体。
type ConnFlags struct {
	Type    string
	Host    string
	Port    int
	Un      string
	Pw      string
	DBName  string
	SubType string
}

// ToConn 将连接 flags 转为 engine.DBConnInfo。
func (cf *ConnFlags) ToConn() *engine.DBConnInfo {
	if cf.Type == "" {
		return nil
	}
	return &engine.DBConnInfo{
		DBConnection: def.DBConnection{
			Type:    cf.Type,
			SubType: cf.SubType,
			Host:    cf.Host,
			Port:    cf.Port,
			Un:      cf.Un,
			Pw:      cf.Pw,
			DBName:  cf.DBName,
		},
	}
}
