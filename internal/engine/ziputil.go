package engine

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// zipDir 将 srcDir 目录打包为 zipPath，zip 内路径相对于 srcDir（即 库名/表名.sql 结构）
func zipDir(srcDir, zipPath string) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(w, src)
		return err
	})
}

// unzip 解压 zipPath 到 destDir（防 zip-slip），返回解压后的目录
func unzip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	for _, f := range r.File {
		target := filepath.Join(destDir, filepath.FromSlash(f.Name))
		// 防 zip-slip
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("非法的 zip 路径: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(target, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// sanitizeName 清理用于文件/目录名的字符串
func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", `"`, "_", "<", "_", ">", "_", "|", "_", " ", "_")
	return replacer.Replace(name)
}
