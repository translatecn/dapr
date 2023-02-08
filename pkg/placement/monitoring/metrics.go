// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package monitoring

import (
	"context"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	diag_utils "github.com/dapr/dapr/pkg/diagnostics/utils"
)

var (
	// 报告给placement服务的运行时总数。
	runtimesTotal = stats.Int64(
		"placement/runtimes_total",
		"The total number of runtimes reported to placement service.",
		stats.UnitDimensionless)
	// 报告给placement服务的 Actor运行时总数。
	actorRuntimesTotal = stats.Int64(
		"placement/actor_runtimes_total",
		"The total number of actor runtimes reported to placement service.",
		stats.UnitDimensionless)

	noKeys []tag.Key
)

// RecordRuntimesCount 记录 connected runtimes.
func RecordRuntimesCount(count int) {
	stats.Record(context.Background(), runtimesTotal.M(int64(count)))
}

// RecordActorRuntimesCount 记录 valid actor runtimes.
func RecordActorRuntimesCount(count int) {
	stats.Record(context.Background(), actorRuntimesTotal.M(int64(count)))
}

// InitMetrics 初始化 placement 服务指标.
func InitMetrics() error {
	err := view.Register(
		diag_utils.NewMeasureView(runtimesTotal, noKeys, view.LastValue()),
		diag_utils.NewMeasureView(actorRuntimesTotal, noKeys, view.LastValue()),
	)

	return err
}
