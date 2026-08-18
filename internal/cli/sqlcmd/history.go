package sqlcmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/peterh/liner"
)

const (
	maxHistoryEntries = 10000
	historyFileName   = "query_history"
)

func historyFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".dbimpex", historyFileName)
}

func loadHistory(line *liner.State, path string) {
	if path == "" {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var entries []string
	for scanner.Scan() {
		text := scanner.Text()
		if text != "" {
			entries = append(entries, text)
		}
	}
	if len(entries) > maxHistoryEntries {
		entries = entries[len(entries)-maxHistoryEntries:]
	}
	if err := scanner.Err(); err != nil {
		return
	}
	for _, e := range slices.Backward(entries) {
		line.AppendHistory(e)
	}
}

func saveHistory(line *liner.State, path string) {
	if path == "" {
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}

	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()

	// liner 的历史记录通过 ReadHistory 写回
	// 简化实现：只保存最近 maxHistoryEntries 条
	tmpFile := path + ".tmp"
	tf, err := os.Create(tmpFile)
	if err != nil {
		return
	}

	// 用 liner 的 WriteHistory 保存
	_, err = line.WriteHistory(tf)
	tf.Close()
	if err != nil {
		os.Remove(tmpFile)
		return
	}

	// 过滤敏感 SQL
	filtered, err := os.Open(tmpFile)
	if err != nil {
		os.Remove(tmpFile)
		return
	}
	defer filtered.Close()

	scanner := bufio.NewScanner(filtered)
	seen := make(map[string]bool)
	count := 0
	for scanner.Scan() {
		text := scanner.Text()
		if text != "" && !seen[text] && !isSensitiveSQL(text) {
			fmt.Fprintln(f, text)
			seen[text] = true
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		os.Remove(tmpFile)
		return
	}
	os.Remove(tmpFile)
}

func isSensitiveSQL(sql string) bool {
	upper := strings.ToUpper(sql)
	sensitiveKeywords := []string{
		"IDENTIFIED BY", "PASSWORD", "SET PASSWORD",
		"CREATE USER", "ALTER USER",
	}
	for _, kw := range sensitiveKeywords {
		if strings.Contains(upper, kw) {
			return true
		}
	}
	return false
}
