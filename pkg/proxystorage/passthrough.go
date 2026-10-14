// Package proxystorage — 单 SG 查询 HTTP 层直接透传。
// metricql 扩展函数（如 increase_pure）不被 PromQL parser 识别，
// tag_filter 恰好匹配 1 个 ServerGroup 时直接转发给下游，绕过 Engine。
// 作者：王春
// 日期：2026-10-09
package proxystorage

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/jacksontj/promxy/pkg/middleware"
	"github.com/jacksontj/promxy/pkg/servergroup"
)

// NewPassthroughHandler 单 SG 查询透传：查询端点 + tag_filter 恰好匹配 1 个 SG
// 时用 ReverseProxy 直接转发，绕过 PromQL Engine；未命中放行 next。
// routePrefix 在转发前从请求路径剥掉，避免下游收到带前缀路径 404。
// 链序（外→内）：TagParser → ProxyHeaders → Passthrough → Router。
func (p *ProxyStorage) NewPassthroughHandler(next http.Handler, routePrefix string) http.Handler {
	routePrefix = "/" + strings.Trim(routePrefix, "/")
	prefix := strings.TrimSuffix(routePrefix, "/") // "/" → ""，根前缀无需剥

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ① 端点白名单
		if !isQueryPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// ② tag 过滤（与 Engine 路径同一函数，行为一致）
		reqTags := middleware.GetRequestTags(r.Context())
		filtered := FilterServerGroupsByTags(p.GetState().sgs, reqTags)
		if len(filtered) != 1 {
			next.ServeHTTP(w, r)
			return
		}
		sg := filtered[0]

		// ③ 已决策：忽略装饰器（AddLabel/InjectMatchers 等），直接透传

		// ④ 构造 target base URL
		target := sgTargetURL(sg)
		if target == nil {
			next.ServeHTTP(w, r)
			return
		}

		logrus.Infof("whole-query pushdown sg=%s", sg.Cfg.Name)

		// ⑤ Transport = SG 自身，复用 TLS/auth/header 链
		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.Transport = sg

		// ⑥ 剥掉 route-prefix 再转发，避免下游 404；克隆请求不改原始 r
		r2 := r.Clone(r.Context())
		r2.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
		if r.URL.RawPath != "" {
			r2.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, prefix)
		}
		// ⑦ 剥离客户端凭证：Authorization 已存在时 BasicAuthRoundTripper
		// 不会注入 SG 配置的 basic_auth（vendor http_config.go:851 仅在头
		// 缺失时注入），客户端凭证会穿透到下游导致 401。删掉后由 SG
		// Transport 链统一注入下游凭证。
		r2.Header.Del("Authorization")
		proxy.ServeHTTP(w, r2)
		// 下游不可用默认 502，不兜底
	})
}

// isQueryPath 是否为透传端点。后缀匹配，兼容 route-prefix 场景；
// label/{name}/values 含变量路径段，用 前缀+后缀 组合匹配。
func isQueryPath(p string) bool {
	return strings.HasSuffix(p, "/api/v1/query") ||
		strings.HasSuffix(p, "/api/v1/query_range") ||
		strings.HasSuffix(p, "/api/v1/series") ||
		strings.HasSuffix(p, "/api/v1/labels") ||
		(strings.Contains(p, "/api/v1/label/") && strings.HasSuffix(p, "/values"))
}

// sgTargetURL 返回 SG 第一个 target 的 base URL。
// Targets 仅存 host:port，scheme/pathPrefix 从 Cfg 补全；
// 服务发现未就绪时退回 OriginalURLs[0]。
func sgTargetURL(sg *servergroup.ServerGroup) *url.URL {
	st := sg.State()
	if st == nil || len(st.Targets) == 0 {
		if len(sg.OriginalURLs) > 0 {
			if u, err := url.Parse(sg.OriginalURLs[0]); err == nil {
				return u
			}
		}
		return nil
	}
	return &url.URL{
		Scheme: sg.Cfg.Scheme,
		Host:   st.Targets[0],
		Path:   sg.Cfg.PathPrefix,
	}
}
