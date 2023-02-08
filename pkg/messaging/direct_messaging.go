// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package messaging

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/valyala/fasthttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	nr "github.com/dapr/components-contrib/nameresolution"
	"github.com/dapr/kit/logger"

	"github.com/dapr/dapr/pkg/channel"
	"github.com/dapr/dapr/pkg/config"
	diag "github.com/dapr/dapr/pkg/diagnostics"
	diag_utils "github.com/dapr/dapr/pkg/diagnostics/utils"
	"github.com/dapr/dapr/pkg/modes"
	"github.com/dapr/dapr/pkg/retry"
	"github.com/dapr/dapr/utils"

	invokev1 "github.com/dapr/dapr/pkg/messaging/v1"
	internalv1pb "github.com/dapr/dapr/pkg/proto/internals/v1"
)

var log = logger.NewLogger("dapr.runtime.direct_messaging")

// messageClientConnection is the function type to connect to the other
// applications to send the message using service invocation.
type messageClientConnection func(ctx context.Context, address, id string, namespace string, skipTLS, recreateIfExists, enableSSL bool, customOpts ...grpc.DialOption) (*grpc.ClientConn, error)

// DirectMessaging 调用远程目标应用的接口
type DirectMessaging interface {
	Invoke(ctx context.Context, targetAppID string, req *invokev1.InvokeMethodRequest) (*invokev1.InvokeMethodResponse, error)
}

type directMessaging struct {
	appChannel          channel.AppChannel
	connectionCreatorFn messageClientConnection
	appID               string
	mode                modes.DaprMode
	grpcPort            int
	namespace           string
	resolver            nr.Resolver
	tracingSpec         config.TracingSpec
	hostAddress         string
	hostName            string
	maxRequestBodySize  int
	proxy               Proxy
	readBufferSize      int
}

type remoteApp struct {
	id        string
	namespace string
	address   string
}

// NewDirectMessaging 返回一个消息直达的API, todo: 干什么用呢
func NewDirectMessaging(
	appID, namespace string, // mesoid  			`dp-61b7fa0d5c5ca0f638670680-executorapp-4f9b5-787779868f-krfxp`,
	port int, mode modes.DaprMode, // 50002  kubernetes
	appChannel channel.AppChannel,
	clientConnFn messageClientConnection,
	resolver nr.Resolver, //
	tracingSpec config.TracingSpec,
	maxRequestBodySize int, //4
	proxy Proxy, readBufferSize int, // nil 4
	streamRequestBody bool, // false
) DirectMessaging {
	hAddr, _ := utils.GetHostAddress() // 本地地址
	hName, _ := os.Hostname()          // ls-Pro.local

	dm := &directMessaging{
		appChannel:          appChannel,
		connectionCreatorFn: clientConnFn,
		appID:               appID,
		mode:                mode,
		grpcPort:            port,
		namespace:           namespace,
		resolver:            resolver,
		tracingSpec:         tracingSpec,
		hostAddress:         hAddr,
		hostName:            hName + ".cluster.local",// myself
		maxRequestBodySize:  maxRequestBodySize,
		proxy:               proxy,
		readBufferSize:      readBufferSize,
	}

	if proxy != nil {
		proxy.SetRemoteAppFn(dm.getRemoteApp)
		proxy.SetTelemetryFn(dm.setContextSpan)
	}

	return dm
}

// Invoke 接收一个消息请求并调用一个应用程序，可以是本地的也可以是远程的。
func (d *directMessaging) Invoke(ctx context.Context, targetAppID string, req *invokev1.InvokeMethodRequest) (*invokev1.InvokeMethodResponse, error) {
	app, err := d.getRemoteApp(targetAppID) // 远程应用的  域名  结构体
	if err != nil {
		return nil, err
	}

	if app.id == d.appID && app.namespace == d.namespace {
		return d.invokeLocal(ctx, req)
	}
	// 说明请求 与本地代理的应用不一样，需要转发到其他应用 , 封装invokeRemote 闭包一些 重试策略
	return d.invokeWithRetry(ctx, retry.DefaultLinearRetryCount, retry.DefaultLinearBackoffInterval, app, d.invokeRemote, req)
}

// requestAppIDAndNamespace 接收一个应用程序ID，并返回应用程序ID、命名空间和错误。
func (d *directMessaging) requestAppIDAndNamespace(targetAppID string) (string, string, error) {
	items := strings.Split(targetAppID, ".")
	if len(items) == 1 {
		return targetAppID, d.namespace, nil
	} else if len(items) == 2 {
		return items[0], items[1], nil
	} else {
		return "", "", errors.Errorf("invalid app id %s", targetAppID)
	}
}

// invokeWithRetry 将调用一个远程端点进行指定次数的重试，并且只在瞬时失败的情况下重试。
// TODO: check why https://github.com/grpc-ecosystem/go-grpc-middleware/blob/master/retry/examples_test.go
// 当目标服务器关闭时，不会恢复连接。
func (d *directMessaging) invokeWithRetry(
	ctx context.Context,
	numRetries int, // 重试次数
	backoffInterval time.Duration, // 重试间隔
	app remoteApp, // app id 、namespace、address[app 域名]
	fn func(ctx context.Context, appID, namespace, appAddress string, req *invokev1.InvokeMethodRequest) (*invokev1.InvokeMethodResponse, error),
	req *invokev1.InvokeMethodRequest) (*invokev1.InvokeMethodResponse, error) {
	for i := 0; i < numRetries; i++ {
		resp, err := fn(ctx, app.id, app.namespace, app.address, req)
		if err == nil {
			return resp, nil
		}
		log.Debugf("重试次数: %d, grpc调用失败, ns: %s, addr: %s, appid: %s, err: %s", i+1, app.namespace, app.address, app.id, err.Error())
		time.Sleep(backoffInterval)

		code := status.Code(err) // 解码错误信息
		if code == codes.Unavailable || code == codes.Unauthenticated {
			// 不可达、未认证
			_, connerr := d.connectionCreatorFn(context.TODO(), app.address, app.id, app.namespace, false, true, false)
			if connerr != nil {
				return nil, connerr
			}
			continue
		}
		return resp, err
	}
	return nil, errors.Errorf("调用远端应用失败 %s 重试次数 %v", app.id, numRetries)
}

// 直接调用本地代理的服务
func (d *directMessaging) invokeLocal(ctx context.Context, req *invokev1.InvokeMethodRequest) (*invokev1.InvokeMethodResponse, error) {
	if d.appChannel == nil {
		return nil, errors.New("无法调用本地端点：应用程序通道未初始化")
	}

	return d.appChannel.InvokeMethod(ctx, req)
}

func (d *directMessaging) setContextSpan(ctx context.Context) context.Context {
	span := diag_utils.SpanFromContext(ctx)
	ctx = diag.SpanContextToGRPCMetadata(ctx, span.SpanContext())

	return ctx
}

func (d *directMessaging) invokeRemote(ctx context.Context, appID, namespace, appAddress string, req *invokev1.InvokeMethodRequest) (*invokev1.InvokeMethodResponse, error) {
	// 创建与远端dapr的连接, k8s 内直接通过域名进行的连接，因此不会拿到所有被调用程序的地址
	conn, err := d.connectionCreatorFn(context.TODO(), appAddress, appID, namespace, false, false, false)
	if err != nil {
		return nil, err
	}

	ctx = d.setContextSpan(ctx)
	//在元数据中添加转发的头信息
	d.addForwardedHeadersToMetadata(req)
	d.addDestinationAppIDHeaderToMetadata(appID, req) // 将目标应用ID 添加到header

	clientV1 := internalv1pb.NewServiceInvocationClient(conn)

	var opts []grpc.CallOption
	// 最大接收的消息大小，最大发送的消息大小
	// pkg/runtime/cli.go:188
	opts = append(opts, grpc.MaxCallRecvMsgSize(d.maxRequestBodySize*1024*1024), grpc.MaxCallSendMsgSize(d.maxRequestBodySize*1024*1024))
	resp, err := clientV1.CallLocal(ctx, req.Proto(), opts...)
	if err != nil {
		return nil, err
	}

	return invokev1.InternalInvokeResponse(resp)
}

func (d *directMessaging) addDestinationAppIDHeaderToMetadata(appID string, req *invokev1.InvokeMethodRequest) {
	req.Metadata()[invokev1.DestinationIDHeader] = &internalv1pb.ListStringValue{
		Values: []string{appID},
	}
}

//在元数据中添加转发的头信息
func (d *directMessaging) addForwardedHeadersToMetadata(req *invokev1.InvokeMethodRequest) {
	metadata := req.Metadata()
	//map[Accept:values:"*/*" Accept-Encoding:values:"gzip, deflate" Connection:values:"keep-alive" Content-Length:values:"25" Host:values:"localhost:3500" User-Agent:values:"python-requests/2.26.0"]
	//
	var forwardedHeaderValue string

	addOrCreate := func(header string, value string) {
		if metadata[header] == nil {
			metadata[header] = &internalv1pb.ListStringValue{
				Values: []string{value},
			}
		} else {
			metadata[header].Values = append(metadata[header].Values, value)
		}
	}

	if d.hostAddress != "" { // 调用方地址
		// Add X-Forwarded-For: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/X-Forwarded-For
		addOrCreate(fasthttp.HeaderXForwardedFor, d.hostAddress)
		forwardedHeaderValue += "for=" + d.hostAddress + ";by=" + d.hostAddress + ";"
	}

	if d.hostName != "" {
		// Add X-Forwarded-Host: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/X-Forwarded-Host
		addOrCreate(fasthttp.HeaderXForwardedHost, d.hostName)
		forwardedHeaderValue += "host=" + d.hostName
	}
	//for=10.10.16.232;by=10.10.16.232;host=liushuodeMacBook-Pro.local   //  发起方、代理方、代理方主机名
	// Add Forwarded header: https://tools.ietf.org/html/rfc7239
	addOrCreate(fasthttp.HeaderForwarded, forwardedHeaderValue)
}

// 获取目标应用地址[域名]
func (d *directMessaging) getRemoteApp(appID string) (remoteApp, error) {
	id, namespace, err := d.requestAppIDAndNamespace(appID)
	if err != nil {
		return remoteApp{}, err
	}

	request := nr.ResolveRequest{ID: id, Namespace: namespace, Port: d.grpcPort} // grpc内部通信的端口
	// 会调用域名解析组件解析域名
	address, err := d.resolver.ResolveID(request)                                // dp-61c03c5f8ea49c26debd26a6-executorapp-dapr.mesoid.svc.cluster.local:50002
	if err != nil {
		return remoteApp{}, err
	}

	return remoteApp{
		namespace: namespace,
		id:        id,
		address:   address,
	}, nil
}
