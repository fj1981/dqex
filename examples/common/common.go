// Package common 提供 examples 共用的小工具：从环境变量构造连接信息与错误退出。
package common

import (
	"fmt"
	"os"
	"strconv"

	dqex "github.com/fj1981/dqex"
)

// Conn 从环境变量构造连接信息，变量名前缀 prefix（如 "SRC"、"TGT"）：
//
//	{P}_TYPE（必填，mysql/postgresql/oracle）、{P}_HOST（必填）、{P}_PORT、
//	{P}_USER、{P}_PASSWORD、{P}_DBNAME
func Conn(prefix string) (dqex.DBConnInfo, error) {
	typ := os.Getenv(prefix + "_TYPE")
	host := os.Getenv(prefix + "_HOST")
	if typ == "" || host == "" {
		return dqex.DBConnInfo{}, fmt.Errorf("missing env: %s_TYPE and %s_HOST are required", prefix, prefix)
	}
	port, _ := strconv.Atoi(os.Getenv(prefix + "_PORT"))
	return dqex.NewConn(typ, host, port,
		os.Getenv(prefix+"_USER"), os.Getenv(prefix+"_PASSWORD"), os.Getenv(prefix+"_DBNAME")), nil
}

// Fail 打印错误并退出（SvcError 额外输出错误码，演示按错误码分支处理，3.3）。
func Fail(err error) {
	var se *dqex.SvcError
	if dqex.AsSvcErr(err, &se) {
		fmt.Fprintf(os.Stderr, "failed (code=%d): %s\n", se.Code, se.Msg("zh"))
	} else {
		fmt.Fprintf(os.Stderr, "failed: %v\n", err)
	}
	os.Exit(1)
}
