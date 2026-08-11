package middleware

import (
	"context"
	"net/http"
	"strings"
)

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
		err := r.ParseForm()
		if err == nil { // 解析成功才去读取表单
			formTag := r.PostForm.Get(queryKeyRouteTags)
			if len(formTag) == 0 {
				formTag = r.PostForm.Get(tagLinkParam)
			}
			tagValue = formTag
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
