// Package changelog 嵌入版本升级记录（双语 Markdown），供前端 API 按语言返回。
package changelog

import "embed"

//go:embed *.md
var changelogFS embed.FS

// Get 根据语言标识返回对应语言的 CHANGELOG 内容（lang="en" 返回英文版，其余返回中文）。
func Get(lang string) string {
	name := "CHANGELOG_CN.md"
	if lang == "en" {
		name = "CHANGELOG_EN.md"
	}
	data, err := changelogFS.ReadFile(name)
	if err != nil {
		return ""
	}
	return string(data)
}
