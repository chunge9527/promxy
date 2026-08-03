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
	requestTagsKey tagContextKey = 1
)

// defaultTag is the fallback tag used when no tags are provided
const defaultTag = "default"

const tagLinkParam = "region"

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

	// Get original values for logging
	urlTag := r.URL.Query().Get("routeTags")
	if len(urlTag) == 0 {
		urlTag = r.URL.Query().Get(tagLinkParam)
	}
	headerTag := r.Header.Get("X-ROUTE-TAGS")

	// Priority: URL parameter > Header
	tagValue := ""
	if urlTag != "" {
		tagValue = urlTag
	} else if headerTag != "" {
		tagValue = headerTag
	}

	// Parse and clean tags
	tags := parseTags(tagValue)

	// If no tags provided, use default tag
	if len(tags) == 0 {
		tags = []string{defaultTag}
		tagValue = defaultTag
	}

	// Store tags in context
	ctx := context.WithValue(r.Context(), requestTagsKey, tags)
	t.next.ServeHTTP(w, r.WithContext(ctx))
}

// parseTags parses a comma-separated tag string and returns a cleaned slice of tags
func parseTags(tagString string) []string {
	if tagString == "" {
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
