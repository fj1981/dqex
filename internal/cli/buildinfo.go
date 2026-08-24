//go:build !opensource

package cli

// OpenSourceBuild 是否开源构建（默认关闭：不在"关于"弹窗展示项目 Git 地址与联系方式）。
// 使用 `go build -tags opensource` 编译时，本文件被排除，由 buildinfo_opensource.go 覆盖为 true。
const OpenSourceBuild = false
