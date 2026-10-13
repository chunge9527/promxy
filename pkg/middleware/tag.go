package middleware

import (
	"context"
	"io"
	"net/http"
	"strings"
)

// 作者：王春
// 日期：2026-10-09
// 修正：ParseForm 会读干 form body；在透传场景下 ReverseProxy 转发的 body 会是空，
// 需在 ParseForm 成功后用 PostForm.Encode() 重建 body，同时保留 PostForm 缓存
// 供后续链路（如 Prometheus web api 的 FormValue）继续读取。

// tagContextKey is the context key for storing request tags
type tagContextKey int

const (
	// requestTagsKey is used to store the parsed tags in request context
	requestTagsKey     tagContextKey = iota
	queryKeyRouteTags                = "routeTags"
	tagLinkParam                     = "region"
	headerKeyRouteTags               = "X-ROUTE-TAGS"
	// defaultTag is the fallback tag used when no tags are provided
	defaultTag = "default"
)

// TagParser is a middleware that parses tags from URL query parameter or HTTP header
type TagParser struct {
	next http.Handler
}

// NewTagParser creates a new TagParser middleware
func NewTagParser(next http.Handler) *TagParser {
	return &TagParser{
		next: next,
	}
}

// ServeHTTP implements http.Handler
func (t *TagParser) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var tagValue string

	// 1. 优先读取 URL Query: routeTags -> region
	urlTag := r.URL.Query().Get(queryKeyRouteTags)
	if len(urlTag) == 0 {
		urlTag = r.URL.Query().Get(tagLinkParam)
	}
	tagValue = urlTag

	// 2. Query无值，并且是可携带表单的请求方法，才解析POST Form
	if len(tagValue) == 0 && (r.Method == http.MethodPost || r.Method == http.MethodPut) {
		// 解析 application/x-www-form-urlencoded 表单
		// logging.go:160 ：`ApacheLoggingHandler.ServeHTTP` 在向下传递请求 之前 调用了`r.ParseForm()` （它需要`r.Form` 来生成访问日志里的 query 字段，见 logging.go:169 ）
		err := r.ParseForm()
		if err == nil { // 解析成功才去读取表单
			formTag := r.PostForm.Get(queryKeyRouteTags)
			if len(formTag) == 0 {
				formTag = r.PostForm.Get(tagLinkParam)
			}
			tagValue = formTag
		}
	}

	// 重建 form body：本中间件或更外层的 ApacheLoggingHandler（logging.go
	// 的 r.ParseForm()）都会把 application/x-www-form-urlencoded 的 body 读干。
	if len(r.PostForm) > 0 {
		encoded := r.PostForm.Encode()
		r.Body = io.NopCloser(strings.NewReader(encoded))
		r.ContentLength = int64(len(encoded))
		r.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(encoded)), nil
		}
	}
	// 3. 表单仍然无值，读取Header
	if len(tagValue) == 0 {
		tagValue = r.Header.Get(headerKeyRouteTags)
	}

	// Parse and clean tags
	tags := parseTags(tagValue)

	// If no tags provided, use default tag
	if len(tags) == 0 {
		tags = []string{defaultTag}
	}

	// Store tags in context
	ctx := context.WithValue(r.Context(), requestTagsKey, tags)
	t.next.ServeHTTP(w, r.WithContext(ctx))
}

// parseTags parses a comma-separated tag string and returns a cleaned slice of tags
func parseTags(tagString string) []string {
	if len(tagString) == 0 {
		return nil
	}

	parts := strings.Split(tagString, ",")
	tags := make([]string, 0, len(parts))

	for _, part := range parts {
		tag := strings.TrimSpace(part)
		if tag != "" {
			tags = append(tags, tag)
		}
	}

	return tags
}

// GetRequestTags retrieves the tags from the request context
func GetRequestTags(ctx context.Context) []string {
	v := ctx.Value(requestTagsKey)
	if v == nil {
		return nil
	}

	tags, ok := v.([]string)
	if !ok {
		return nil
	}

	return tags
}
