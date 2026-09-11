//go:build !opensource

package cli

// OpenSourceBuild 开源构建开关：默认关闭，Web 端"关于"弹窗不展示项目链接。
// 与 buildinfo_opensource.go 中定义互斥，经 `go build -tags opensource` 切换为 true。
const OpenSourceBuild = false
