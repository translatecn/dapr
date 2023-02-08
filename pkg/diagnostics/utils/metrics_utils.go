// Package utils ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------
// diagnostics 诊断
package utils

import (
	"context"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

// NewMeasureView 创建 opencensus 视图实例 by stats.Measure.
func NewMeasureView(measure stats.Measure, keys []tag.Key, aggregation *view.Aggregation) *view.View {
	return &view.View{
		Name:        measure.Name(),
		Description: measure.Description(),
		Measure:     measure, // 指标
		TagKeys:     keys,// 标签
		Aggregation: aggregation,
	}
}

// WithTags converts tag key and value pairs to tag.Mutator array.
// WithTags(key1, value1, key2, value2) returns
// []tag.Mutator{tag.Upsert(key1, value1), tag.Upsert(key2, value2)}.
func WithTags(opts ...interface{}) []tag.Mutator {
	tagMutators := []tag.Mutator{}
	for i := 0; i < len(opts)-1; i += 2 {
		key, ok := opts[i].(tag.Key)
		if !ok {
			break
		}
		value, ok := opts[i+1].(string)
		if !ok {
			break
		}
		// skip if value is empty
		if value != "" {
			tagMutators = append(tagMutators, tag.Upsert(key, value))
		}
	}
	return tagMutators
}

// AddTagKeyToCtx 分配opencensus标签键值到上下文。
func AddTagKeyToCtx(ctx context.Context, key tag.Key, value string) context.Context {
	// return if value is not given
	if value == "" {
		return ctx
	}

	newCtx, err := tag.New(ctx, tag.Upsert(key, value))
	if err != nil {
		// return original if adding tagkey is failed.
		return ctx
	}

	return newCtx
}

// AddNewTagKey 未存在的视图添加TagKeys
func AddNewTagKey(views []*view.View, key *tag.Key) []*view.View {
	for _, v := range views {
		v.TagKeys = append(v.TagKeys, *key)
	}

	return views
}
