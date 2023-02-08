// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"strconv"
	"time"

	nethttp "net/http"

	"github.com/dapr/dapr/pkg/channel"
	"github.com/dapr/dapr/pkg/config"
	diag "github.com/dapr/dapr/pkg/diagnostics"
	diag_utils "github.com/dapr/dapr/pkg/diagnostics/utils"
	invokev1 "github.com/dapr/dapr/pkg/messaging/v1"
	commonv1pb "github.com/dapr/dapr/pkg/proto/common/v1"
	internalv1pb "github.com/dapr/dapr/pkg/proto/internals/v1"
	auth "github.com/dapr/dapr/pkg/runtime/security"
	jsoniter "github.com/json-iterator/go"
	"github.com/valyala/fasthttp"
	"go.opencensus.io/plugin/ochttp/propagation/tracecontext"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// HTTPStatusCode 是dapr http通道状态代码。
	HTTPStatusCode = "http.status_code"
	httpScheme     = "http"
	httpsScheme    = "https"

	appConfigEndpoint = "dapr/config"
)

// Channel APP channel 的http实现
type Channel struct {
	client              *fasthttp.Client
	baseAddress         string             // eg http://127.0.0.1:8000
	ch                  chan int           // 用于限制并发
	tracingSpec         config.TracingSpec // 追踪信息
	appHeaderToken      string             // app token
	json                jsoniter.API
	maxResponseBodySize int
}

// CreateLocalChannel 创建http应用channel
// nolint:gosec
func CreateLocalChannel(port, maxConcurrency int, spec config.TracingSpec, sslEnabled bool, maxRequestBodySize int, readBufferSize int) (channel.AppChannel, error) {
	scheme := httpScheme
	if sslEnabled {
		scheme = httpsScheme
	}

	c := &Channel{
		client: &fasthttp.Client{
			MaxConnsPerHost:           1000000,
			MaxIdemponentCallAttempts: 0,                                // 重试次数 0
			MaxResponseBodySize:       maxRequestBodySize * 1024 * 1024, // 请求体大小 , M
			ReadBufferSize:            readBufferSize * 1024,            // K
		},
		baseAddress:         fmt.Sprintf("%s://%s:%d", scheme, channel.DefaultChannelAddress, port),
		tracingSpec:         spec, // 追踪的配置
		appHeaderToken:      auth.GetAppToken(),
		json:                jsoniter.ConfigFastest, // 序列化模块
		maxResponseBodySize: maxRequestBodySize,
	}

	if sslEnabled {
		// 控制客户端是否验证服务器的证书链和主机名。
		// 如果InsecureSkipVerify为真，crypto/tls接受服务器提交的任何证书和该证书中的任何主机名。
		c.client.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}
	// 默认是-1
	if maxConcurrency > 0 {
		c.ch = make(chan int, maxConcurrency)
	}

	return c, nil
}

// GetBaseAddress 返回应用地址
func (h *Channel) GetBaseAddress() string {
	return h.baseAddress
}

// GetAppConfig 获取应用的配置
// GET http://localhost:<app_port>/dapr/config
func (h *Channel) GetAppConfig() (*config.ApplicationConfig, error) {
	// 没有传输任何请求体数据
	req := invokev1.NewInvokeMethodRequest(appConfigEndpoint) // "dapr/config"
	req.WithHTTPExtension(nethttp.MethodGet, "")
	req.WithRawData(nil, invokev1.JSONContentType)

	// TODO Propagate context
	ctx := context.Background()
	resp, err := h.InvokeMethod(ctx, req)
	if err != nil {
		return nil, err
	}

	var config config.ApplicationConfig

	if resp.Status().Code != nethttp.StatusOK {
		return &config, nil
	}

	_, body := resp.RawData()
	if err = h.json.Unmarshal(body, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// InvokeMethod 调用
func (h *Channel) InvokeMethod(ctx context.Context, req *invokev1.InvokeMethodRequest) (*invokev1.InvokeMethodResponse, error) {
	// 必须调用了 pkg/channel/http/http_channel.go:96
	httpExt := req.Message().GetHttpExtension()
	if httpExt == nil {
		return nil, status.Error(codes.InvalidArgument, "missing HTTP extension field")
	}
	// 必须调用了 pkg/channel/http/http_channel.go:96
	if httpExt.GetVerb() == commonv1pb.HTTPExtension_NONE { // HTTPExtension_GET
		return nil, status.Error(codes.InvalidArgument, "无效的 HTTP 请求方法")
	}

	var rsp *invokev1.InvokeMethodResponse
	var err error
	switch req.APIVersion() {
	//  pkg/channel/http/http_channel.go:95 版本默认就是 APIVersion_V1
	case internalv1pb.APIVersion_V1:
		rsp, err = h.invokeMethodV1(ctx, req)

	default:
		// 拒绝不支持的版本,不会走到这
		err = status.Error(codes.Unimplemented, fmt.Sprintf("不支持的版本: %d", req.APIVersion()))
	}

	return rsp, err
}

func (h *Channel) invokeMethodV1(ctx context.Context, req *invokev1.InvokeMethodRequest) (*invokev1.InvokeMethodResponse, error) {
	channelReq := h.constructRequest(ctx, req)

	if h.ch != nil {
		h.ch <- 1
	}

	// 在发送请求时记录度量值
	verb := string(channelReq.Header.Method())
	diag.DefaultHTTPMonitoring.ClientRequestStarted(ctx, verb, req.Message().Method, int64(len(req.Message().Data.GetValue())))
	startRequest := time.Now()

	// 向用户应用发送请求
	resp := fasthttp.AcquireResponse()
	err := h.client.Do(channelReq, resp)
	defer func() {
		fasthttp.ReleaseRequest(channelReq)
		fasthttp.ReleaseResponse(resp)
	}()

	elapsedMs := float64(time.Since(startRequest) / time.Millisecond)

	if err != nil {
		diag.DefaultHTTPMonitoring.ClientRequestCompleted(ctx, verb, req.Message().GetMethod(), strconv.Itoa(nethttp.StatusInternalServerError), int64(resp.Header.ContentLength()), elapsedMs)
		return nil, err
	}

	if h.ch != nil {
		<-h.ch
	}

	rsp := h.parseChannelResponse(req, resp)
	diag.DefaultHTTPMonitoring.ClientRequestCompleted(ctx, verb, req.Message().GetMethod(),
		strconv.Itoa(int(rsp.Status().Code)), int64(resp.Header.ContentLength()), elapsedMs)

	return rsp, nil
}

// 由 proto 结构体信息，构建 fasthttp 请求信息
func (h *Channel) constructRequest(ctx context.Context, req *invokev1.InvokeMethodRequest) *fasthttp.Request {
	channelReq := fasthttp.AcquireRequest()

	// 构建应用程序通道URI: VERB http://localhost:3000/method?query1=value1
	uri := fmt.Sprintf("%s/%s", h.baseAddress, req.Message().GetMethod())
	channelReq.SetRequestURI(uri)
	channelReq.URI().SetQueryString(req.EncodeHTTPQueryString())
	channelReq.Header.SetMethod(req.Message().HttpExtension.Verb.String())

	// 恢复header
	invokev1.InternalMetadataToHTTPHeader(ctx, req.Metadata(), channelReq.Header.Set)

	// HTTP客户端需要注入traceparent头，以获得正确的追踪堆栈。
	span := diag_utils.SpanFromContext(ctx)
	tp, ts := new(tracecontext.HTTPFormat).SpanContextToHeaders(span.SpanContext()) // 将SpanContext序列化为traceparent和tracestate头文件。
	// tp 00-8cd311a613d20f585e33f9cdee3bd5f6-9f5e10cf8ace2a2b-00
	// ts ""
	channelReq.Header.Set("traceparent", tp)
	if ts != "" {
		channelReq.Header.Set("tracestate", ts)
	}

	if h.appHeaderToken != "" {
		channelReq.Header.Set(auth.APITokenHeader, h.appHeaderToken)
	}

	// Set Content body and types
	contentType, body := req.RawData()
	channelReq.Header.SetContentType(contentType)
	channelReq.SetBody(body)

	return channelReq
}

// 解析用户应用返回消息
func (h *Channel) parseChannelResponse(req *invokev1.InvokeMethodRequest, resp *fasthttp.Response) *invokev1.InvokeMethodResponse {
	var statusCode int
	var contentType string
	var body []byte

	statusCode = resp.StatusCode()
	contentType = (string)(resp.Header.ContentType())
	body = resp.Body()

	// 转换状态码
	rsp := invokev1.NewInvokeMethodResponse(int32(statusCode), "", nil)
	rsp.WithFastHTTPHeaders(&resp.Header).WithRawData(body, contentType)

	return rsp
}
