package http

import (
	"fmt"
	"github.com/dapr/dapr/pkg/messages"
	"github.com/dapr/dapr/pkg/messaging"
	invokev1 "github.com/dapr/dapr/pkg/messaging/v1"
	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
)

//构建直接信息传递的端点
func (a *api) constructDirectMessagingEndpoints() []Endpoint {
	return []Endpoint{
		{
			Methods: []string{router.MethodWild},
			Route:   "invoke/{id}/method/{method:*}",
			Alias:   "{method:*}",
			Version: apiVersionV1,
			Handler: a.onDirectMessage,
		},
	}
}
func (a *api) SetDirectMessaging(directMessaging messaging.DirectMessaging) {
	a.directMessaging = directMessaging
}

// 直接消息传递
func (a *api) onDirectMessage(reqCtx *fasthttp.RequestCtx) {
	targetID := a.findTargetID(reqCtx) //  获取请求中的目标应用ID
	if targetID == "" {
		msg := NewErrorResponse("ERR_DIRECT_INVOKE", messages.ErrDirectInvokeNoAppID)
		respond(reqCtx, withError(fasthttp.StatusNotFound, msg))
		return
	}
	// 请求方法
	verb := strings.ToUpper(string(reqCtx.Method()))
	//调用目标方法名
	invokeMethodName := reqCtx.UserValue(methodParam).(string)

	// 运行初始化时，就创建了对应的结构体
	if a.directMessaging == nil {
		msg := NewErrorResponse("ERR_DIRECT_INVOKE", messages.ErrDirectInvokeNotReady)
		respond(reqCtx, withError(fasthttp.StatusInternalServerError, msg))
		return
	}

	// 构建内部调用方法请求
	req := invokev1.NewInvokeMethodRequest(invokeMethodName).WithHTTPExtension(verb, reqCtx.QueryArgs().String())
	req.WithRawData(reqCtx.Request.Body(), string(reqCtx.Request.Header.ContentType()))
	// 设置请求头
	req.WithFastHTTPHeaders(&reqCtx.Request.Header)

	resp, err := a.directMessaging.Invoke(reqCtx, targetID, req)
	// err 不代表用户应用程序的响应
	if err != nil {
		// 在被叫方应用的Allowlists策略可以返回一个Permission Denied错误。 对于其他一切，将其视为gRPC传输错误
		statusCode := fasthttp.StatusInternalServerError
		if status.Code(err) == codes.PermissionDenied {
			statusCode = invokev1.HTTPStatusFromCode(codes.PermissionDenied)
		}
		msg := NewErrorResponse("ERR_DIRECT_INVOKE", fmt.Sprintf(messages.ErrDirectInvoke, targetID, err))
		respond(reqCtx, withError(statusCode, msg))
		return
	}

	invokev1.InternalMetadataToHTTPHeader(reqCtx, resp.Headers(), reqCtx.Response.Header.Set)
	contentType, body := resp.RawData()
	reqCtx.Response.Header.SetContentType(contentType)

	// 构造反应
	statusCode := int(resp.Status().Code)
	if !resp.IsHTTPResponse() { // 如果不是http响应码
		statusCode = invokev1.HTTPStatusFromCode(codes.Code(statusCode))
		if statusCode != fasthttp.StatusOK {
			if body, err = invokev1.ProtobufToJSON(resp.Status()); err != nil {
				msg := NewErrorResponse("ERR_MALFORMED_RESPONSE", err.Error())
				respond(reqCtx, withError(fasthttp.StatusInternalServerError, msg))
				return
			}
		}
	}
	respond(reqCtx, with(statusCode, body))
}
