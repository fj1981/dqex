package service

import (
	"strings"

	"github.com/fj1981/infrakit/pkg/cygin"
)

// resolveContributors 解析任务引用的贡献者（配置层 + 代理层 两段式）：
// 任务条目只需 Type（+ IDs），Export/Import 回调为空时按 Type 匹配
// WithContributors/LibraryOptions.Contributors 注册的模板补齐；
// 无任何可用回调视为未注册，返回 ErrCtbUnknown。
// 同 Type 多条目合并为一条（IDs 取并集），避免回调重复执行与目录重复写入。
func (s *Service) resolveContributors(items []Contributor) ([]Contributor, error) {
	if len(items) == 0 {
		return nil, nil
	}
	resolved := make([]Contributor, 0, len(items))
	index := map[string]int{} // Type -> resolved 下标
	for _, it := range items {
		typ := strings.TrimSpace(it.Type)
		if typ == "" {
			return nil, cygin.NewError(ErrCtbUnknown, cygin.WithErrPrint(), cygin.WithErrDetailf("contributor type is empty"))
		}
		if it.Export != nil || it.Import != nil {
			// 任务自带回调（直接调用 Client.RunExport 且自行填充回调的场景），不去重
			resolved = append(resolved, it)
			continue
		}
		var tpl *Contributor
		for i := range s.contributors {
			if s.contributors[i].Type == typ {
				tpl = &s.contributors[i]
				break
			}
		}
		if tpl == nil || (tpl.Export == nil && tpl.Import == nil) {
			return nil, cygin.NewError(ErrCtbUnknown, cygin.WithErrPrint(), cygin.WithErrDetailf("contributor type %q not registered", typ))
		}
		if idx, ok := index[typ]; ok {
			// 同 Type 合并：IDs 并集
			seen := map[string]bool{}
			for _, id := range resolved[idx].IDs {
				seen[id] = true
			}
			for _, id := range it.IDs {
				if !seen[id] {
					resolved[idx].IDs = append(resolved[idx].IDs, id)
					seen[id] = true
				}
			}
			continue
		}
		cp := *tpl
		cp.IDs = it.IDs
		index[typ] = len(resolved)
		resolved = append(resolved, cp)
	}
	return resolved, nil
}
