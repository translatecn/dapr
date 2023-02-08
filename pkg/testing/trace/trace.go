package trace

import (
	"github.com/dapr/kit/logger"
	"go.opencensus.io/trace"
	"strconv"
)

// NewStringExporter 返回一个新的字符串导出器实例。
//
// 在我们想验证跟踪传播的测试场景中，它非常有用。
func NewStringExporter(buffer *string, logger logger.Logger) *Exporter {
	return &Exporter{
		Buffer: buffer,
		logger: logger,
	}
}

// Exporter OpenCensus 字符串导出器
type Exporter struct {
	Buffer *string
	logger logger.Logger
}

// ExportSpan
func (se *Exporter) ExportSpan(sd *trace.SpanData) {
	*se.Buffer = strconv.Itoa(int(sd.Status.Code))
}

// Register creates a new string exporter endpoint and reporter.
func (se *Exporter) Register(daprID string) {
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})
	trace.RegisterExporter(se)
}
