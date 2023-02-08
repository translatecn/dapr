package runtime

import (
	"context"
	"fmt"
	"github.com/dapr/components-contrib/bindings"
	components_v1alpha1 "github.com/dapr/dapr/pkg/apis/components/v1alpha1"
	diag "github.com/dapr/dapr/pkg/diagnostics"
	invokev1 "github.com/dapr/dapr/pkg/messaging/v1"
	runtimev1pb "github.com/dapr/dapr/pkg/proto/runtime/v1"
	"github.com/pkg/errors"
	"go.opencensus.io/trace"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	nethttp "net/http"
	"strings"
	"time"
)

func (a *DaprRuntime) initBinding(c components_v1alpha1.Component) error {
	// 判断本地注册中有没有该输出绑定的实现类
	if a.bindingsRegistry.HasOutputBinding(c.Spec.Type, c.Spec.Version) {
		if err := a.initOutputBinding(c); err != nil { // Init
			log.Errorf("初始化输出绑定失败: %s", err)
			return err
		}
	}
	// 判断本地注册中有没有该输入绑定的实现类
	if a.bindingsRegistry.HasInputBinding(c.Spec.Type, c.Spec.Version) {
		if err := a.initInputBinding(c); err != nil {
			log.Errorf("初始化输入绑定失败: %s", err)
			return err
		}
	}
	return nil
}

func (a *DaprRuntime) initInputBinding(c components_v1alpha1.Component) error {
	binding, err := a.bindingsRegistry.CreateInputBinding(c.Spec.Type, c.Spec.Version) // 绑定结构体
	if err != nil {
		log.Warnf("创建输入绑定失败 %s (%s/%s): %s", c.ObjectMeta.Name, c.Spec.Type, c.Spec.Version, err)
		diag.DefaultMonitoring.ComponentInitFailed(c.Spec.Type, "creation")
		return err
	}
	err = binding.Init(bindings.Metadata{
		Properties: a.convertMetadataItemsToProperties(c.Spec.Metadata),
		Name:       c.ObjectMeta.Name,
	})
	if err != nil {
		log.Errorf("初始化输入绑定失败 %s (%s/%s): %s", c.ObjectMeta.Name, c.Spec.Type, c.Spec.Version, err)
		diag.DefaultMonitoring.ComponentInitFailed(c.Spec.Type, "init")
		return err
	}

	log.Infof("初始化输入绑定成功 %s (%s/%s)", c.ObjectMeta.Name, c.Spec.Type, c.Spec.Version)
	a.inputBindingRoutes[c.Name] = c.Name
	for _, item := range c.Spec.Metadata {
		if item.Name == "route" {
			a.inputBindingRoutes[c.ObjectMeta.Name] = item.Value.String()
		}
	}
	a.inputBindings[c.Name] = binding
	diag.DefaultMonitoring.ComponentInitialized(c.Spec.Type)
	return nil
}

func (a *DaprRuntime) initOutputBinding(c components_v1alpha1.Component) error {
	binding, err := a.bindingsRegistry.CreateOutputBinding(c.Spec.Type, c.Spec.Version) // 返回绑定的结构体
	if err != nil {
		log.Warnf("创建输出绑定失败%s (%s/%s): %s", c.ObjectMeta.Name, c.Spec.Type, c.Spec.Version, err)
		diag.DefaultMonitoring.ComponentInitFailed(c.Spec.Type, "creation")
		return err
	}

	if binding != nil {
		err := binding.Init(bindings.Metadata{
			Properties: a.convertMetadataItemsToProperties(c.Spec.Metadata),
			Name:       c.ObjectMeta.Name,
		})
		if err != nil {
			log.Errorf("初始化输出绑定失败 %s (%s/%s): %s", c.ObjectMeta.Name, c.Spec.Type, c.Spec.Version, err)
			diag.DefaultMonitoring.ComponentInitFailed(c.Spec.Type, "init")
			return err
		}
		log.Infof("初始化输出绑定成功 %s (%s/%s)", c.ObjectMeta.Name, c.Spec.Type, c.Spec.Version)
		a.outputBindings[c.ObjectMeta.Name] = binding
		diag.DefaultMonitoring.ComponentInitialized(c.Spec.Type)
	}
	return nil
}

func (a *DaprRuntime) getSubscribedBindingsGRPC() []string {
	client := runtimev1pb.NewAppCallbackClient(a.grpc.AppClient)
	resp, err := client.ListInputBindings(context.Background(), &emptypb.Empty{})
	bindings := []string{}

	if err == nil && resp != nil {
		bindings = resp.Bindings
	}
	return bindings
}

func (a *DaprRuntime) isAppSubscribedToBinding(binding string) bool {
	// 如果是gRPC，则在从应用程序返回的绑定列表中寻找该绑定。
	if a.runtimeConfig.ApplicationProtocol == GRPCProtocol {
		if a.subscribeBindingList == nil {
			a.subscribeBindingList = a.getSubscribedBindingsGRPC()
		}
		for _, b := range a.subscribeBindingList {
			if b == binding {
				return true
			}
		}
	} else if a.runtimeConfig.ApplicationProtocol == HTTPProtocol {
		//  如果是http，则在从应用程序返回的绑定列表中寻找该绑定。
		path := a.inputBindingRoutes[binding]
		req := invokev1.NewInvokeMethodRequest(path)
		req.WithHTTPExtension(nethttp.MethodOptions, "")
		req.WithRawData(nil, invokev1.JSONContentType)

		// TODO: Propagate Context
		ctx := context.Background()
		resp, err := a.appChannel.InvokeMethod(ctx, req)
		if err != nil {
			log.Fatalf("无法调用输入绑定订阅端点的OPTIONS方法 %q: %w", path, err)
		}
		code := resp.Status().Code
		// 是200 获取方法不允许
		return code/100 == 2 || code == nethttp.StatusMethodNotAllowed
	}
	return false
}

func (a *DaprRuntime) sendToOutputBinding(name string, req *bindings.InvokeRequest) (*bindings.InvokeResponse, error) {
	if req.Operation == "" {
		return nil, errors.New("请求数据丢失了操作类型 字段")
	}
	// 输出绑定的名称
	if binding, ok := a.outputBindings[name]; ok {
		ops := binding.Operations() // 支持的操作类型
		for _, o := range ops {
			if o == req.Operation {
				return binding.Invoke(req)
			}
		}
		supported := make([]string, 0, len(ops))
		for _, o := range ops {
			supported = append(supported, string(o))
		}
		return nil, errors.Errorf("绑定 %s 不支持 %s操作. 支持的操作:%s", name, req.Operation, strings.Join(supported, " "))
	}
	return nil, errors.Errorf("不能找到输出绑定 %s", name)
}

func (a *DaprRuntime) sendBatchOutputBindingsParallel(to []string, data []byte) {
	for _, dst := range to {
		go func(name string) {
			_, err := a.sendToOutputBinding(name, &bindings.InvokeRequest{
				Data:      data,
				Operation: bindings.CreateOperation,
			})
			if err != nil {
				log.Error(err)
			}
		}(dst)
	}
}

func (a *DaprRuntime) sendBatchOutputBindingsSequential(to []string, data []byte) error {
	for _, dst := range to {
		_, err := a.sendToOutputBinding(dst, &bindings.InvokeRequest{
			Data:      data,
			Operation: bindings.CreateOperation,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *DaprRuntime) sendBindingEventToApp(bindingName string, data []byte, metadata map[string]string) ([]byte, error) {
	var response bindings.AppResponse
	spanName := fmt.Sprintf("bindings/%s", bindingName)
	ctx, span := diag.StartInternalCallbackSpan(context.Background(), spanName, trace.SpanContext{}, a.globalConfig.Spec.TracingSpec)

	var appResponseBody []byte

	if a.runtimeConfig.ApplicationProtocol == GRPCProtocol {
		ctx = diag.SpanContextToGRPCMetadata(ctx, span.SpanContext())
		client := runtimev1pb.NewAppCallbackClient(a.grpc.AppClient)
		req := &runtimev1pb.BindingEventRequest{
			Name:     bindingName,
			Data:     data,
			Metadata: metadata,
		}
		start := time.Now()
		resp, err := client.OnBindingEvent(ctx, req)
		if span != nil {
			m := diag.ConstructInputBindingSpanAttributes(
				bindingName,
				"/dapr.proto.runtime.v1.AppCallback/OnBindingEvent")
			diag.AddAttributesToSpan(span, m)
			diag.UpdateSpanStatusFromGRPCError(span, err)
			span.End()
		}
		if diag.DefaultGRPCMonitoring.IsEnabled() {
			diag.DefaultGRPCMonitoring.ServerRequestSent(ctx,
				"/dapr.proto.runtime.v1.AppCallback/OnBindingEvent",
				status.Code(err).String(),
				int64(len(resp.GetData())), start)
		}

		if err != nil {
			var body []byte
			if resp != nil {
				body = resp.Data
			}
			return nil, errors.Wrap(err, fmt.Sprintf("error invoking app, body: %s", string(body)))
		}
		if resp != nil {
			if resp.Concurrency == runtimev1pb.BindingEventResponse_PARALLEL {
				response.Concurrency = bindingsConcurrencyParallel
			} else {
				response.Concurrency = bindingsConcurrencySequential
			}

			response.To = resp.To

			if resp.Data != nil {
				appResponseBody = resp.Data

				var d interface{}
				err := a.json.Unmarshal(resp.Data, &d)
				if err == nil {
					response.Data = d
				}
			}
		}
	} else if a.runtimeConfig.ApplicationProtocol == HTTPProtocol {
		path := a.inputBindingRoutes[bindingName]
		req := invokev1.NewInvokeMethodRequest(path)
		req.WithHTTPExtension(nethttp.MethodPost, "")
		req.WithRawData(data, invokev1.JSONContentType)

		reqMetadata := map[string][]string{}
		for k, v := range metadata {
			reqMetadata[k] = []string{v}
		}
		req.WithMetadata(reqMetadata)

		resp, err := a.appChannel.InvokeMethod(ctx, req)
		if err != nil {
			return nil, errors.Wrap(err, "调用用户程序出错")
		}

		if span != nil {
			m := diag.ConstructInputBindingSpanAttributes(
				bindingName,
				fmt.Sprintf("%s /%s", nethttp.MethodPost, bindingName))
			diag.AddAttributesToSpan(span, m)
			diag.UpdateSpanStatusFromHTTPStatus(span, int(resp.Status().Code))
			span.End()
		}
		// ::TODO report metrics for http, such as grpc
		if resp.Status().Code != nethttp.StatusOK {
			_, body := resp.RawData()
			return nil, errors.Errorf("未能向http应用通道发送绑定事件，状态代码为: %d body: %s", resp.Status().Code, string(body))
		}

		if resp.Message().Data != nil && len(resp.Message().Data.Value) > 0 {
			appResponseBody = resp.Message().Data.Value
		}
	}

	if len(response.State) > 0 || len(response.To) > 0 {
		if err := a.onAppResponse(&response); err != nil {
			log.Errorf("执行应用程序响应时出错: %s", err)
		}
	}

	return appResponseBody, nil
}

func (a *DaprRuntime) readFromBinding(name string, binding bindings.InputBinding) error {
	err := binding.Read(func(resp *bindings.ReadResponse) ([]byte, error) {
		if resp != nil {
			b, err := a.sendBindingEventToApp(name, resp.Data, resp.Metadata)
			if err != nil {
				log.Debugf("来自应用程序消费者的绑定错误 [%s]: %s", name, err)
				return nil, err
			}
			return b, err
		}
		return nil, nil
	})
	return err
}

func (a *DaprRuntime) startReadingFromBindings() error {
	if a.appChannel == nil {
		return errors.New("App通道未初始化")
	}
	for name, binding := range a.inputBindings { // 本地初始化的链接
		go func(name string, binding bindings.InputBinding) {
			// 对每一个输入绑定启动一个go routinue
			if !a.isAppSubscribedToBinding(name) { // 判断应用是有没有实现对应的路由函数  todo 如果应用程序只实现了option而没有实现post
				log.Infof("App没有订阅绑定 %s.", name)
				return
			}

			err := a.readFromBinding(name, binding)
			if err != nil {
				log.Errorf("从输入绑定读取错误%s: %s", name, err)
			}
		}(name, binding)
	}
	return nil
}
