package service

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ---- 产物统一存取器 ----
//
// 产物（exports 导出包 / compares 对比报告）统一以「逻辑路径」<prefix>/<name> 表达，
// 两种运行模式一步写到位，历史记录、下载、清理不再感知存储介质：
//
//   - 库模式注入对象存储（WithArtifactStore）：直接上传 <prefix>/<name> 对象，本地不落
//     终态文件。zip/xlsx 等需本地组装的产物先产在 tmp 工作区，组装完成后经
//     artifactLocalPut 做唯一一次流式上传并清理工作区——不存在「落 A 再搬 B」的二段式。
//   - 独立运行（默认）：读写 DataDir/<prefix>/<name> 本地目录，与目录名同构。
//
// 边界（保持本地语义，不走存取器）：
//   - tmp/、uploads/：临时区，生命周期短；
//   - 目录产物（导出 Compress=false 的明细目录，桌面 open-dir 场景）与用户自定义
//     --output 路径：真实本地路径，下载/清理仍按本地文件处理。
const (
	artifactPrefixExports  = "exports"
	artifactPrefixCompares = "compares"
)

// isArtifactLogicalPath 判定 OutputPath 是否为逻辑路径（<prefix>/<name> 形式）。
// 目录产物与用户自定义输出为真实本地路径，不在其列。
func isArtifactLogicalPath(p string) bool {
	cleaned := filepath.ToSlash(strings.TrimSpace(p))
	for _, prefix := range []string{artifactPrefixExports, artifactPrefixCompares} {
		if cleaned == prefix || strings.HasPrefix(cleaned, prefix+"/") {
			return true
		}
	}
	return false
}

// artifactBucketArgs 对象存储 bucket 参数（空 = store 默认 bucket）
func (s *Service) artifactBucketArgs() []string {
	if s.artifactBucket == "" {
		return nil
	}
	return []string{s.artifactBucket}
}

// artifactContentType 按扩展名推断对象 Content-Type（local provider 忽略）
func artifactContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".json":
		return "application/json"
	case ".zip":
		return "application/zip"
	case ".gz":
		return "application/gzip"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	default:
		return "application/octet-stream"
	}
}

// artifactLocalDir 本地模式下前缀对应的受管目录
func (s *Service) artifactLocalDir(prefix string) string {
	switch prefix {
	case artifactPrefixExports:
		return s.persist.ExportDir()
	case artifactPrefixCompares:
		return s.persist.CompareDir()
	case snapshotObjectPrefix:
		return s.persist.SnapshotDir()
	default:
		return filepath.Join(s.persist.BaseDir(), prefix)
	}
}

// artifactPut 写入产物（内存数据），返回逻辑路径 <prefix>/<name>。
// 单通道：对象存储模式直接上传（不落本地终态），独立模式写受管目录。
func (s *Service) artifactPut(prefix, name string, data []byte) (string, error) {
	if s.artifactStore != nil {
		_, err := s.artifactStore.Upload(context.Background(), prefix+"/"+name,
			strings.NewReader(string(data)), int64(len(data)), artifactContentType(name), s.artifactBucketArgs()...)
		if err != nil {
			return "", err
		}
	} else {
		dir := s.artifactLocalDir(prefix)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return "", err
		}
	}
	return prefix + "/" + name, nil
}

// artifactLocalPut 将本地已生成的产物文件归置为终态，返回逻辑路径与文件大小。
// 对象存储模式：从源文件做唯一一次流式上传，成功后删除源文件（tmp 工作区清理）；
// 独立模式：移入对应受管目录（同分区 rename，跨设备回退复制+删除）。
func (s *Service) artifactLocalPut(prefix, name string, srcPath string) (string, int64, error) {
	st, err := os.Stat(srcPath)
	if err != nil {
		return "", 0, err
	}
	if s.artifactStore != nil {
		f, err := os.Open(srcPath)
		if err != nil {
			return "", 0, err
		}
		_, err = s.artifactStore.Upload(context.Background(), prefix+"/"+name,
			f, st.Size(), artifactContentType(name), s.artifactBucketArgs()...)
		closeErr := f.Close()
		if err != nil {
			return "", 0, err
		}
		if closeErr != nil {
			return "", 0, closeErr
		}
		_ = os.Remove(srcPath) // 上传成功后清理 tmp 工作区源文件
	} else {
		dir := s.artifactLocalDir(prefix)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", 0, err
		}
		dst := filepath.Join(dir, name)
		if err := os.Rename(srcPath, dst); err != nil {
			// 跨设备 rename 失败回退复制+删除
			if err = copyFile(srcPath, dst); err != nil {
				return "", 0, err
			}
			_ = os.Remove(srcPath)
		}
	}
	return prefix + "/" + name, st.Size(), nil
}

// artifactOpen 打开产物读取流（下载接口用），返回 reader 与大小（对象存储模式大小未知时为 -1）。
func (s *Service) artifactOpen(logical string) (io.ReadCloser, int64, error) {
	if s.artifactStore != nil {
		rc, err := s.artifactStore.Download(context.Background(), logical, s.artifactBucketArgs()...)
		if err != nil {
			return nil, -1, err
		}
		return rc, -1, nil
	}
	f, err := os.Open(s.artifactLocalPath(logical))
	if err != nil {
		return nil, -1, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, -1, err
	}
	return f, st.Size(), nil
}

// artifactGet 读取产物全部内容（对比报告等小文件）
func (s *Service) artifactGet(logical string) ([]byte, error) {
	rc, _, err := s.artifactOpen(logical)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// artifactRemove 删除产物文件（受管目录/对象存储内），不存在时静默返回。
// 目录产物与用户自定义本地路径不走此方法（DeleteHistory 侧按形态分流）。
func (s *Service) artifactRemove(logical string) error {
	if s.artifactStore != nil {
		err := s.artifactStore.Delete(context.Background(), logical, s.artifactBucketArgs()...)
		if err != nil && !isSnapshotObjectNotFound(err) {
			return err
		}
		return nil
	}
	err := os.Remove(s.artifactLocalPath(logical))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// artifactLocalPath 逻辑路径 → 本地受管路径（仅独立模式使用）
func (s *Service) artifactLocalPath(logical string) string {
	prefix, name := splitArtifactPath(logical)
	return filepath.Join(s.artifactLocalDir(prefix), name)
}

// relocateExportArtifact 将 engine 产出的导出/字典产物归置为终态，返回逻辑路径与大小。
// 单文件产物（zip/xlsx）经 artifactLocalPut 一步到位（对象存储模式唯一一次流式上传）；
// 目录产物（Compress=false 的明细目录，桌面 open-dir 场景）保留本地，原样返回（大小 0，由调用方 stat）。
func (s *Service) relocateExportArtifact(p string) (string, int64, error) {
	st, err := os.Stat(p)
	if err != nil {
		return "", 0, err
	}
	if st.IsDir() {
		return p, 0, nil
	}
	return s.artifactLocalPut(artifactPrefixExports, filepath.Base(p), p)
}

// artifactWorkDir 产物组装工作区（对象存储模式下 engine 产物的临时落点）
func (s *Service) artifactWorkDir(kind string) (string, error) {
	dir := filepath.Join(s.persist.TempDir(), "artifact-"+kind)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// splitArtifactPath 拆分逻辑路径为前缀与文件名
func splitArtifactPath(logical string) (prefix, name string) {
	cleaned := filepath.ToSlash(strings.TrimSpace(logical))
	if i := strings.Index(cleaned, "/"); i > 0 {
		return cleaned[:i], cleaned[i+1:]
	}
	return "", cleaned
}

// IsArtifactLogicalPath 判定 OutputPath 是否为逻辑路径（<prefix>/<name>），供 Web 层分流
func IsArtifactLogicalPath(p string) bool {
	return isArtifactLogicalPath(p)
}

// OpenArtifact 打开产物读取流（Web 下载接口流式返回用），返回 reader 与大小（未知为 -1）。
// 仅接受逻辑路径；目录产物与用户自定义本地路径应由调用方走本地文件语义。
func (s *Service) OpenArtifact(logical string) (io.ReadCloser, int64, error) {
	if !isArtifactLogicalPath(logical) {
		return nil, -1, os.ErrInvalid
	}
	return s.artifactOpen(logical)
}

// LocalArtifactPath 将产物路径换算为本地文件系统路径：逻辑路径按受管目录展开
// （对象存储模式无本地终态文件，返回错误）；真实本地路径（目录产物/用户自定义输出）原样返回。
func (s *Service) LocalArtifactPath(p string) (string, error) {
	if !isArtifactLogicalPath(p) {
		return p, nil
	}
	if s.artifactStore != nil {
		return "", os.ErrNotExist
	}
	return s.artifactLocalPath(p), nil
}

// copyFile 复制文件（rename 跨设备回退用）
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
