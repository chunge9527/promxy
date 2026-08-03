package proxystorage

import (
	"context"
	"strings"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/sirupsen/logrus"

	"github.com/jacksontj/promxy/pkg/middleware"
	"github.com/jacksontj/promxy/pkg/promapi"
	"github.com/jacksontj/promxy/pkg/promclient"
	"github.com/jacksontj/promxy/pkg/servergroup"
)

// tagFilterAPI wraps a MultiAPI and filters server groups based on tags from context
type tagFilterAPI struct {
	sgs      []*servergroup.ServerGroup
	multiAPI *promclient.MultiAPI
}

// newTagFilterAPI creates a new tagFilterAPI
func newTagFilterAPI(sgs []*servergroup.ServerGroup, multiAPI *promclient.MultiAPI) *tagFilterAPI {
	return &tagFilterAPI{
		sgs:      sgs,
		multiAPI: multiAPI,
	}
}

// filterServerGroups filters server groups based on tags from context
// Matching rule: if any tag from the request matches any tag in the server group's GetAllTags(),
// the server group is included. Matching is case-insensitive.
func (t *tagFilterAPI) filterServerGroups(ctx context.Context) []promclient.API {
	reqTags := middleware.GetRequestTags(ctx)

	if len(reqTags) == 0 {
		// No tags in context, return all server groups
		apis := make([]promclient.API, len(t.sgs))
		for i, sg := range t.sgs {
			apis[i] = sg
		}
		return apis
	}

	// Build a map of request tags for quick lookup (case-insensitive)
	reqTagMap := make(map[string]bool)
	for _, tag := range reqTags {
		reqTagMap[strings.ToLower(tag)] = true
	}

	// Log debug information
	logrus.Debugf("[tag] input tags: %v, total server groups: %d", reqTags, len(t.sgs))

	var filteredAPIs []promclient.API

	for _, sg := range t.sgs {
		sgTags := sg.Cfg.AllTagFilters

		// Check if any tag matches (case-insensitive)
		match := false
		for _, sgTag := range sgTags {
			if reqTagMap[strings.ToLower(sgTag)] {
				match = true
				break
			}
		}

		if match {
			filteredAPIs = append(filteredAPIs, sg)
			logrus.Infof("[tag] matched server group: %s, tags: %v", sg.Cfg.Name, sgTags)
		}
	}

	logrus.Debugf("[tag] matched %d server groups out of %d", len(filteredAPIs), len(t.sgs))

	return filteredAPIs
}

// getFilteredMultiAPI returns a MultiAPI with filtered server groups
func (t *tagFilterAPI) getFilteredMultiAPI(ctx context.Context) promclient.API {
	filteredAPIs := t.filterServerGroups(ctx)

	if len(filteredAPIs) == 0 {
		// No matching server groups, return empty API
		logrus.Debugf("[tag] no server groups matched, returning empty API")
		return emptyAPI{}
	}

	if len(filteredAPIs) == len(t.sgs) {
		// All server groups matched, use original multiAPI
		return t.multiAPI
	}

	// Create a new MultiAPI with filtered server groups
	// Use the same antiAffinity and other settings as the original
	multiAPI, err := promclient.NewMultiAPI(filteredAPIs, model.TimeFromUnix(0), false, nil, len(filteredAPIs), false)
	if err != nil {
		logrus.Errorf("[tag] error creating filtered MultiAPI: %v", err)
		return t.multiAPI
	}

	return multiAPI
}

// Query implements promclient.API
func (t *tagFilterAPI) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	return t.getFilteredMultiAPI(ctx).Query(ctx, query, ts)
}

// QueryRange implements promclient.API
func (t *tagFilterAPI) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	return t.getFilteredMultiAPI(ctx).QueryRange(ctx, query, r)
}

// Series implements promclient.API
func (t *tagFilterAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	return t.getFilteredMultiAPI(ctx).Series(ctx, matches, startTime, endTime)
}

// LabelValues implements promclient.API
func (t *tagFilterAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	return t.getFilteredMultiAPI(ctx).LabelValues(ctx, label, matchers, startTime, endTime)
}

// LabelNames implements promclient.API
func (t *tagFilterAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	return t.getFilteredMultiAPI(ctx).LabelNames(ctx, matchers, startTime, endTime)
}

// GetValue implements promclient.API
func (t *tagFilterAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	return t.getFilteredMultiAPI(ctx).GetValue(ctx, start, end, matchers)
}

// Metadata implements promclient.API
func (t *tagFilterAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	return t.getFilteredMultiAPI(ctx).Metadata(ctx, metric, limit)
}

// QueryExemplars implements promclient.API
func (t *tagFilterAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	return t.getFilteredMultiAPI(ctx).QueryExemplars(ctx, query, startTime, endTime)
}

// emptyAPI is a no-op API that returns empty results
type emptyAPI struct{}

func (emptyAPI) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	return promapi.NewSeriesSet(nil, nil, nil)
}

func (emptyAPI) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	return promapi.NewSeriesSet(nil, nil, nil)
}

func (emptyAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	return nil, nil, nil
}

func (emptyAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	return nil, nil, nil
}

func (emptyAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	return nil, nil, nil
}

func (emptyAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	return promapi.NewSeriesSet(nil, nil, nil)
}

func (emptyAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	return nil, nil
}

func (emptyAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	return nil, nil
}
