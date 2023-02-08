// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package runtime

import (
	"go.opencensus.io/trace"
)

// traceExporterStore 允许我们捕获导出器注册的指标。
// 这是有必要的，因为OpenCensus库只暴露了全局方法，用于 出口商的注册。
type traceExporterStore interface {
	// RegisterExporter 注册导出器
	RegisterExporter(exporter trace.Exporter)
}

// openCensusExporterStore
// 是traceExporterStore的一个实现 ，它利用了OpenCensus库的全局exporer存储（`trace'）。
type openCensusExporterStore struct{}

// RegisterExporter 使用OpenCensus的全局注册来实现 traceExporterStore。
func (s openCensusExporterStore) RegisterExporter(exporter trace.Exporter) {
	trace.RegisterExporter(exporter)
}

// fakeTraceExporterStore
//实现traceExporterStore，只记录被注册/应用的出口商和配置。这仅仅是为了在单元测试中使用。
type fakeTraceExporterStore struct {
	exporters []trace.Exporter
}

// RegisterExporter 记录给定的trace.Exporter。
func (r *fakeTraceExporterStore) RegisterExporter(exporter trace.Exporter) {
	r.exporters = append(r.exporters, exporter)
}
