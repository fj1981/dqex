//go:build opensource

package cli

// OpenSourceBuild 开源构建开关：启用后 Web 端"关于"弹窗展示项目 Git 地址与联系方式。
// 与 buildinfo.go 中默认定义互斥，经 `go build -tags opensource` 生效。
const OpenSourceBuild = true
