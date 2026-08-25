package web

import (
	"net"
	"strings"
	"sync"
	"time"

	"github.com/fj1981/infrakit/pkg/cylog"
)

// ---- 访问来源白名单（--allow / 配置 web.allow） ----

// allowRule 白名单单条规则：IP / CIDR / 域名（域名按需解析为 IP 后比对）
type allowRule struct {
	ip     net.IP
	cidr   *net.IPNet
	domain string
}

// accessFilter 访问来源过滤器：本机回环始终放行（避免误锁自己）；
// 无规则 = 拒绝所有外部来源（对外暴露必须显式配置白名单）
type accessFilter struct {
	rules []allowRule
	mu    sync.RWMutex
	cache map[string]domainCache // 域名 → 解析结果缓存
}

type domainCache struct {
	ips      []net.IP
	expireAt time.Time
}

// domainResolveTTL 域名解析缓存时长：到期重新解析，适应 DNS 记录变更
const domainResolveTTL = 5 * time.Minute

// newAccessFilter 解析白名单条目（IP / CIDR / 域名）；非法条目记录日志后跳过
func newAccessFilter(entries []string) *accessFilter {
	f := &accessFilter{}
	f.Set(entries)
	return f
}

// Set 运行时整体替换白名单规则（配置保存后热生效，无需重启）。
// 非法条目记录日志后跳过；同时清空域名解析缓存，避免命中旧记录。
func (f *accessFilter) Set(entries []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rules := make([]allowRule, 0, len(entries))
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		switch {
		case strings.Contains(e, "/"):
			if _, cidr, err := net.ParseCIDR(e); err == nil {
				rules = append(rules, allowRule{cidr: cidr})
				continue
			}
		case net.ParseIP(e) != nil:
			rules = append(rules, allowRule{ip: net.ParseIP(e)})
			continue
		case looksLikeDomain(e):
			rules = append(rules, allowRule{domain: strings.ToLower(e)})
			continue
		}
		cylog.Warnf("访问白名单条目无效，已忽略: %s（支持 IP / CIDR / 域名）", e)
	}
	f.rules = rules
	f.cache = map[string]domainCache{}
}

// looksLikeDomain 粗判域名形态：含点号或为 localhost（其余情况视为非法条目）
func looksLikeDomain(s string) bool {
	return s == "localhost" || strings.Contains(s, ".")
}

// allow 判定客户端 IP 是否允许访问。
// 安全策略：本机回环始终放行；白名单为空时一律拒绝外部来源——
// 对外暴露（非回环监听）必须显式配置白名单（--allow / web.allow），
// 避免令牌泄露后任意来源 IP 都能访问（noAuth 场景更是完全裸奔）。
func (f *accessFilter) allow(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() {
		return true
	}
	// 快照读取当前规则：允许 Set 热更新时安全替换，且避免持读锁调用 resolve（其内部需写锁）造成死锁
	f.mu.RLock()
	rules := make([]allowRule, len(f.rules))
	copy(rules, f.rules)
	f.mu.RUnlock()
	if len(rules) == 0 {
		// 未配置白名单：拒绝所有外部来源（仅本机回环可访问）
		return false
	}
	for _, r := range rules {
		switch {
		case r.ip != nil:
			if r.ip.Equal(ip) {
				return true
			}
		case r.cidr != nil:
			if r.cidr.Contains(ip) {
				return true
			}
		case r.domain != "":
			if f.domainMatches(r.domain, ip) {
				return true
			}
		}
	}
	return false
}

// domainMatches 域名解析出的任一 IP 与客户端 IP 一致即命中
func (f *accessFilter) domainMatches(domain string, ip net.IP) bool {
	for _, resolved := range f.resolve(domain) {
		if resolved.Equal(ip) {
			return true
		}
	}
	return false
}

// resolve 域名解析（带缓存）：失败返回空（该条规则暂不生效，到期后重试）
func (f *accessFilter) resolve(domain string) []net.IP {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.cache[domain]; ok && time.Now().Before(c.expireAt) {
		return c.ips
	}
	ips := []net.IP{}
	if addrs, err := net.LookupIP(domain); err == nil {
		ips = addrs
	} else {
		cylog.Warnf("访问白名单域名解析失败（该条目暂不生效）: %s: %v", domain, err)
	}
	f.cache[domain] = domainCache{ips: ips, expireAt: time.Now().Add(domainResolveTTL)}
	return ips
}

// ---- 认证失败限速（防令牌暴力破解，对外暴露时尤为重要） ----

const (
	authFailLimit  = 10              // 统计窗口内允许的最大失败次数
	authFailWindow = time.Minute     // 失败统计窗口
	authBlockTime  = 5 * time.Minute // 触发后锁定时长
)

// authLimiter 按来源 IP 统计认证失败：窗口内超限则临时锁定
type authLimiter struct {
	mu      sync.Mutex
	records map[string]*authRecord
}

type authRecord struct {
	fails        []time.Time
	blockedUntil time.Time
}

func newAuthLimiter() *authLimiter {
	return &authLimiter{records: map[string]*authRecord{}}
}

// allow 来源是否允许尝试认证（锁定未到期返回 false）
func (l *authLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.records[ip]
	return r == nil || time.Now().After(r.blockedUntil)
}

// fail 记录一次认证失败：窗口内累计超限则锁定并清空计数
func (l *authLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	r := l.records[ip]
	if r == nil {
		r = &authRecord{}
		l.records[ip] = r
	}
	// 清理窗口外的失败记录，仅保留窗口内计数
	cutoff := now.Add(-authFailWindow)
	kept := r.fails[:0]
	for _, t := range r.fails {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	r.fails = append(kept, now)
	if len(r.fails) >= authFailLimit {
		r.blockedUntil = now.Add(authBlockTime)
		r.fails = nil
		cylog.Warnf("认证失败次数过多，来源 %s 已临时锁定 %s", ip, authBlockTime)
	}
}

// pass 认证成功：清除该来源的失败记录
func (l *authLimiter) pass(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.records, ip)
}
