// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package diagnostics

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	diag_utils "github.com/dapr/dapr/pkg/diagnostics/utils"
)

// To track the metrics for fasthttp using opencensus, this implementation is inspired by
// https://github.com/census-instrumentation/opencensus-go/tree/master/plugin/ochttp

// http请求指标的tagkey
var (
	httpStatusCodeKey = tag.MustNewKey("status")
	httpPathKey       = tag.MustNewKey("path")
	httpMethodKey     = tag.MustNewKey("method")
)

// 默认分布
var (
	// 大小分布
	defaultSizeDistribution = view.Distribution(1024, 2048, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864, 268435456, 1073741824, 4294967296)
	// 延迟分布
	defaultLatencyDistribution = view.Distribution(1, 2, 3, 4, 5, 6, 8, 10, 13, 16, 20, 25, 30, 40, 50, 65, 80, 100, 130, 160, 200, 250, 300, 400, 500, 650, 800, 1000, 2000, 5000, 10000, 20000, 50000, 100000)
)

type httpMetrics struct {
	serverRequestCount  *stats.Int64Measure   // 服务器中启动的HTTP请求的数量。
	serverRequestBytes  *stats.Int64Measure   // 如果在服务器中设置为ContentLength（未压缩），则为HTTP请求体大小。
	serverResponseBytes *stats.Int64Measure   // 服务器中的HTTP响应体大小（未压缩）。
	serverLatency       *stats.Float64Measure // 服务器中的HTTP请求端到端延迟
	serverResponseCount *stats.Int64Measure   // HTTP响应的数量

	clientSentBytes        *stats.Int64Measure   // 请求正文中发送的总字节数（不包括头信息）。
	clientReceivedBytes    *stats.Int64Measure   // 响应体中收到的总字节数（不包括头信息，但包括响应体中的错误响应）。
	clientRoundtripLatency *stats.Float64Measure // 整个请求生命周期的时间 从发送请求头的第一个字节到收到响应的最后一个字节的时间，或终端错误
	clientCompletedCount   *stats.Int64Measure   // 已完成的请求数

	appID   string
	enabled bool
}

func newHTTPMetrics() *httpMetrics {
	return &httpMetrics{
		serverRequestCount: stats.Int64(
			"http/server/request_count",
			"Number of HTTP requests started in server.",
			stats.UnitDimensionless),
		serverRequestBytes: stats.Int64(
			"http/server/request_bytes",
			"HTTP request body size if set as ContentLength (uncompressed) in server.",
			stats.UnitBytes),
		serverResponseBytes: stats.Int64(
			"http/server/response_bytes",
			"HTTP response body size (uncompressed) in server.",
			stats.UnitBytes),
		serverLatency: stats.Float64(
			"http/server/latency",
			"HTTP request end to end latency in server.",
			stats.UnitMilliseconds),
		serverResponseCount: stats.Int64(
			"http/server/response_count",
			"The number of HTTP responses",
			stats.UnitDimensionless),
		clientSentBytes: stats.Int64(
			"http/client/sent_bytes",
			"Total bytes sent in request body (not including headers)",
			stats.UnitBytes),
		clientReceivedBytes: stats.Int64(
			"http/client/received_bytes",
			"Total bytes received in response bodies (not including headers but including error responses with bodies)",
			stats.UnitBytes),
		clientRoundtripLatency: stats.Float64(
			"http/client/roundtrip_latency",
			"Time between first byte of request headers sent to last byte of response received, or terminal error",
			stats.UnitMilliseconds),
		clientCompletedCount: stats.Int64(
			"http/client/completed_count",
			"Count of completed requests",
			stats.UnitDimensionless),

		enabled: false,
	}
}

// IsEnabled 是否启用指标监控
func (h *httpMetrics) IsEnabled() bool {
	return h.enabled
}

// ServerRequestReceived 记录指标
func (h *httpMetrics) ServerRequestReceived(ctx context.Context, method, path string, contentSize int64) {
	if h.enabled {
		stats.RecordWithTags(
			ctx,
			diag_utils.WithTags(appIDKey, h.appID, httpPathKey, path, httpMethodKey, method),
			h.serverRequestCount.M(1))
		stats.RecordWithTags(
			ctx, diag_utils.WithTags(appIDKey, h.appID),
			h.serverRequestBytes.M(contentSize))
	}
}

// ServerRequestCompleted 记录指标
func (h *httpMetrics) ServerRequestCompleted(ctx context.Context, method, path, status string, contentSize int64, elapsed float64) {
	if h.enabled {
		stats.RecordWithTags(
			ctx,
			diag_utils.WithTags(appIDKey, h.appID, httpPathKey, path, httpMethodKey, method, httpStatusCodeKey, status),
			h.serverResponseCount.M(1))
		stats.RecordWithTags(
			ctx,
			diag_utils.WithTags(appIDKey, h.appID, httpPathKey, path, httpMethodKey, method, httpStatusCodeKey, status),
			h.serverLatency.M(elapsed))
		stats.RecordWithTags(
			ctx, diag_utils.WithTags(appIDKey, h.appID),
			h.serverResponseBytes.M(contentSize))
	}
}

// ClientRequestStarted 记录指标
func (h *httpMetrics) ClientRequestStarted(ctx context.Context, method, path string, contentSize int64) {
	if h.enabled {
		stats.RecordWithTags(
			ctx,
			diag_utils.WithTags(appIDKey, h.appID, httpPathKey, h.convertPathToMetricLabel(path), httpMethodKey, method),
			h.clientSentBytes.M(contentSize))
	}
}

// ClientRequestCompleted 记录指标
func (h *httpMetrics) ClientRequestCompleted(ctx context.Context, method, path, status string, contentSize int64, elapsed float64) {
	if h.enabled {
		stats.RecordWithTags(
			ctx,
			diag_utils.WithTags(appIDKey, h.appID, httpPathKey, h.convertPathToMetricLabel(path), httpMethodKey, method, httpStatusCodeKey, status),
			h.clientCompletedCount.M(1))
		stats.RecordWithTags(
			ctx,
			diag_utils.WithTags(appIDKey, h.appID, httpPathKey, h.convertPathToMetricLabel(path), httpMethodKey, method, httpStatusCodeKey, status),
			h.clientRoundtripLatency.M(elapsed))
		stats.RecordWithTags(
			ctx, diag_utils.WithTags(appIDKey, h.appID),
			h.clientReceivedBytes.M(contentSize))
	}
}

// Init 添加暴露指标
func (h *httpMetrics) Init(appID string) error {
	h.appID = appID
	h.enabled = true

	tags := []tag.Key{appIDKey}
	return view.Register(
		diag_utils.NewMeasureView(h.serverRequestCount, []tag.Key{appIDKey, httpPathKey, httpMethodKey}, view.Count()),
		diag_utils.NewMeasureView(h.serverRequestBytes, tags, defaultSizeDistribution),
		diag_utils.NewMeasureView(h.serverResponseBytes, tags, defaultSizeDistribution),
		diag_utils.NewMeasureView(h.serverLatency, []tag.Key{appIDKey, httpMethodKey, httpPathKey, httpStatusCodeKey}, defaultLatencyDistribution),
		diag_utils.NewMeasureView(h.serverResponseCount, []tag.Key{appIDKey, httpMethodKey, httpPathKey, httpStatusCodeKey}, view.Count()),
		diag_utils.NewMeasureView(h.clientSentBytes, []tag.Key{appIDKey, httpMethodKey, httpPathKey, httpStatusCodeKey}, defaultSizeDistribution),
		diag_utils.NewMeasureView(h.clientReceivedBytes, tags, defaultSizeDistribution),
		diag_utils.NewMeasureView(h.clientRoundtripLatency, []tag.Key{appIDKey, httpMethodKey, httpPathKey, httpStatusCodeKey}, defaultLatencyDistribution),
		diag_utils.NewMeasureView(h.clientCompletedCount, []tag.Key{appIDKey, httpMethodKey, httpPathKey, httpStatusCodeKey}, view.Count()),
	)
}

// FastHTTPMiddleware 是跟踪http服务器端请求的中间件。
func (h *httpMetrics) FastHTTPMiddleware(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		reqContentSize := ctx.Request.Header.ContentLength()
		if reqContentSize < 0 {
			reqContentSize = 0
		}

		method := string(ctx.Method())
		path := h.convertPathToMetricLabel(string(ctx.Path()))

		// 记录指标
		h.ServerRequestReceived(ctx, method, path, int64(reqContentSize))

		start := time.Now()

		next(ctx)

		status := strconv.Itoa(ctx.Response.StatusCode())
		elapsed := float64(time.Since(start) / time.Millisecond)
		respSize := int64(len(ctx.Response.Body()))
		// 记录指标
		h.ServerRequestCompleted(ctx, method, path, status, respSize, elapsed)
	}
}

// convertPathToMetricLabel 移除URL路径中的变量参数，用于标签空间
// For example, it removes {keys} param from /v1/state/statestore/{keys}.
func (h *httpMetrics) convertPathToMetricLabel(path string) string {
	if path == "" {
		return path
	}

	p := path
	//    v1/state/statestore/{keys}
	if p[0] == '/' {
		p = path[1:]
	}

	// 最多可以分出6个分隔符， 'v1/actors/DemoActor/1/timer/name'
	parsedPath := strings.SplitN(p, "/", 6)

	if len(parsedPath) < 3 {
		return path
	}

	// 用{id}替换 actor ID  - 'actors/DemoActor/1/method/method1'
	if parsedPath[0] == "actors" {
		parsedPath[2] = "{id}"
		return strings.Join(parsedPath, "/")
	}

	switch parsedPath[1] {
	case "state", "secrets":
		// state api: 拼接 3 个元素(v1, state, statestore)   /v1/state/statestore/key
		// secrets api: 拼接 3 个元素(v1, secrets, keyvault)   /v1/secrets/keyvault/name
		return "/" + strings.Join(parsedPath[0:3], "/")

	case "actors":
		if len(parsedPath) < 5 {
			return path
		}
		// 忽略id部分
		parsedPath[3] = "{id}"
		// 拼接 5 个元素(v1, actors, DemoActor, {id}, timer) in /v1/actors/DemoActor/1/timer/name
		return "/" + strings.Join(parsedPath[0:5], "/")
	}

	return path
}
