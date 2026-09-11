// 通用目录布局重排引擎：声明式 glob 规则驱动的路径重排（复制语义）。
// 纯机制层，不含任何业务语义——业务布局映射（如"flow/ → 5_flow/"）由宿主
// 按自身配置推导成规则传入。服务于宿主旧布局与引擎标准布局的双向适配。
package dqex

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// LayoutRule 单条路径重排规则。
// Match 为源相对路径 glob（path.Match 语义，'/' 分隔，不支持跨级 **）；
// Dest 为目标相对路径模板，其中每个 '*' 依次替换为 Match 中各 '*' 捕获段。
// 例：{Match: "flow/*", Dest: "5_flow/*"}、{Match: "camunda.sql", Dest: "1_coe/camunda.sql"}。
type LayoutRule struct {
	Match string
	Dest  string
}

// RelayoutDir 按 rules 将 srcDir 重排复制到 dstDir。
//   - 文件按相对路径（'/' 分隔）依序匹配规则，首个命中者生效；
//   - Dest 中的 '*' 按顺序替换为 Match 捕获段，目标目录自动创建，同名文件覆盖；
//   - 无规则命中的文件：dropUnmatched 为 false 时按原路径复制，true 时丢弃；
//   - srcDir/dstDir 均须为已存在的目录，可相同（原地重排，此时以复制后再删除源文件实现）；
//   - dstDir 不得位于 srcDir 内部（遍历会重复处理已复制的产物），返回错误。
func RelayoutDir(srcDir, dstDir string, rules []LayoutRule, dropUnmatched bool) error {
	return relayoutDir(srcDir, dstDir, rules, dropUnmatched, nil)
}

// RelayoutDirWithMap 同 RelayoutDir，额外通过 observed 回调上报每条"源相对路径 → 目标相对路径"
// 实际映射（含未命中按原路径复制的），便于宿主校验与调试；dst 为空表示该文件被丢弃。
func RelayoutDirWithMap(srcDir, dstDir string, rules []LayoutRule, dropUnmatched bool, observed func(src, dst string)) error {
	return relayoutDir(srcDir, dstDir, rules, dropUnmatched, observed)
}

func relayoutDir(srcDir, dstDir string, rules []LayoutRule, dropUnmatched bool, observed func(src, dst string)) error {
	inPlace := sameDir(srcDir, dstDir)
	if !inPlace && isDirInside(srcDir, dstDir) {
		return fmt.Errorf("dqex: relayoutDir: dstDir %q inside srcDir %q", dstDir, srcDir)
	}
	return filepath.WalkDir(srcDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, p)
		if err != nil {
			return err
		}
		srcRel := filepath.ToSlash(rel)
		dstRel, ok := matchRules(srcRel, rules)
		if !ok {
			if dropUnmatched {
				if observed != nil {
					observed(srcRel, "")
				}
				if inPlace {
					return os.Remove(p)
				}
				return nil
			}
			dstRel = srcRel
		}
		dstPath := filepath.Join(dstDir, filepath.FromSlash(dstRel))
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return err
		}
		if err := copyFile(p, dstPath); err != nil {
			return err
		}
		if observed != nil {
			observed(srcRel, dstRel)
		}
		if inPlace {
			return os.Remove(p)
		}
		return nil
	})
}

// matchRules 按序匹配首个命中规则，返回展开后的目标相对路径。
func matchRules(srcRel string, rules []LayoutRule) (string, bool) {
	for _, r := range rules {
		caps, ok := matchGlob(r.Match, srcRel)
		if !ok {
			continue
		}
		dst := r.Dest
		for _, c := range caps {
			dst = strings.Replace(dst, "*", c, 1)
		}
		return path.Clean(dst), true
	}
	return "", false
}

// matchGlob 单级 glob 匹配，返回 '*' 捕获段（无捕获返回 nil）。
// 仅单个 '*' 支持捕获；多个 '*' 退化为 path.Match 判定（无捕获，Dest 此时不应含 '*'）。
func matchGlob(pattern, name string) ([]string, bool) {
	i := strings.Index(pattern, "*")
	if i < 0 {
		return nil, pattern == name
	}
	prefix, suffix := pattern[:i], pattern[i+1:]
	if strings.Contains(suffix, "*") {
		ok, _ := path.Match(pattern, name)
		return nil, ok
	}
	if len(name) < len(prefix)+len(suffix) ||
		!strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return nil, false
	}
	cap := name[len(prefix) : len(name)-len(suffix)]
	if strings.Contains(cap, "/") {
		return nil, false // '*' 不跨目录级
	}
	return []string{cap}, true
}

func sameDir(a, b string) bool {
	ra, errA := filepath.Abs(a)
	rb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	return ra == rb
}

// isDirInside 判断 inner 是否为 outer 的内部（子）目录（非同一目录）。
func isDirInside(outer, inner string) bool {
	ro, errO := filepath.Abs(outer)
	ri, errI := filepath.Abs(inner)
	if errO != nil || errI != nil {
		return false
	}
	rel, err := filepath.Rel(ro, ri)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

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
	_, err = io.Copy(out, in)
	if err != nil {
		out.Close()
		os.Remove(dst) // 清理部分写入的残留文件
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode().Perm())
}
