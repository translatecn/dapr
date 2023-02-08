package runtime

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/dapr/components-contrib/contenttype"
	contrib_metadata "github.com/dapr/components-contrib/metadata"
	"github.com/dapr/components-contrib/pubsub"
	components_v1alpha1 "github.com/dapr/dapr/pkg/apis/components/v1alpha1"
	diag "github.com/dapr/dapr/pkg/diagnostics"
	invokev1 "github.com/dapr/dapr/pkg/messaging/v1"
	"github.com/dapr/dapr/pkg/modes"
	runtimev1pb "github.com/dapr/dapr/pkg/proto/runtime/v1"
	runtime_pubsub "github.com/dapr/dapr/pkg/runtime/pubsub"
	"github.com/dapr/dapr/pkg/scopes"
	"github.com/pkg/errors"
	"go.opencensus.io/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	nethttp "net/http"
	"strings"
)

func (a *DaprRuntime) getPublishAdapter() runtime_pubsub.Adapter {
	if a.pubSubs == nil || len(a.pubSubs) == 0 {
		return nil
	}
	return a
}
func (a *DaprRuntime) initPubSub(c components_v1alpha1.Component) error {
	pubSub, err := a.pubSubRegistry.Create(c.Spec.Type, c.Spec.Version)
	if err != nil {
		log.Warnf("创建订阅发布发生错误 %s (%s/%s): %s", &c.ObjectMeta.Name, c.Spec.Type, c.Spec.Version, err)
		diag.DefaultMonitoring.ComponentInitFailed(c.Spec.Type, "creation")
		return err
	}

	properties := a.convertMetadataItemsToProperties(c.Spec.Metadata)
	consumerID := strings.TrimSpace(properties["consumerID"])
	if consumerID == "" {
		consumerID = a.runtimeConfig.ID
	}
	properties["consumerID"] = consumerID

	err = pubSub.Init(pubsub.Metadata{
		Properties: properties,
	})
	if err != nil {
		log.Warnf("初始化pubsub出错 %s/%s: %s", c.Spec.Type, c.Spec.Version, err)
		diag.DefaultMonitoring.ComponentInitFailed(c.Spec.Type, "init")
		return err
	}

	pubsubName := c.ObjectMeta.Name

	a.scopedSubscriptions[pubsubName] = scopes.GetScopedTopics(scopes.SubscriptionScopes, a.runtimeConfig.ID, properties)
	a.scopedPublishings[pubsubName] = scopes.GetScopedTopics(scopes.PublishingScopes, a.runtimeConfig.ID, properties)
	a.allowedTopics[pubsubName] = scopes.GetAllowedTopics(properties)
	a.pubSubs[pubsubName] = pubSub
	diag.DefaultMonitoring.ComponentInitialized(c.Spec.Type)

	return nil
}
func (a *DaprRuntime) beginPubSub(pubSubName string, ps pubsub.PubSub) error {
	var publishFunc func(ctx context.Context, msg *pubsubSubscribedMessage) error
	switch a.runtimeConfig.ApplicationProtocol {
	case HTTPProtocol:
		publishFunc = a.publishMessageHTTP
	case GRPCProtocol:
		publishFunc = a.publishMessageGRPC
	}
	topicRoutes, err := a.getTopicRoutes() // 获取订阅的的全部映射信息
	if err != nil {
		return err
	}
	v, ok := topicRoutes[pubSubName] // 获取本pub有没有sub
	if !ok {
		return nil
	}
	// 组件
	for topic, route := range v.routes {
		// 判断pubsub操作 在组件定义里是否允许
		allowed := a.isPubSubOperationAllowed(pubSubName, topic, a.scopedSubscriptions[pubSubName])
		if !allowed {
			log.Warnf("订阅的主题 %s 在pubsub组件 %s 不被允许", topic, pubSubName)
			continue
		}

		log.Debugf("订阅中 topic=%s on pubsub=%s", topic, pubSubName)

		routeMetadata := route.metadata
		err := ps.Subscribe(pubsub.SubscribeRequest{
			Topic:    topic,
			Metadata: route.metadata,
		},
			func(ctx context.Context, msg *pubsub.NewMessage) error {
				if msg.Metadata == nil {
					msg.Metadata = make(map[string]string, 1)
				}
				// pubsubName    组件的名字
				msg.Metadata[pubsubName] = pubSubName

				rawPayload, err := contrib_metadata.IsRawPayload(routeMetadata)
				if err != nil {
					log.Errorf("反序列化pubsub元数据的错误: %s", err)
					return err
				}

				var cloudEvent map[string]interface{}
				data := msg.Data
				if rawPayload {
					// 不是cloud event ;自己封装成event
					cloudEvent = pubsub.FromRawPayload(msg.Data, msg.Topic, pubSubName)
					data, err = a.json.Marshal(cloudEvent)
					if err != nil {
						log.Errorf("在pubsub %s和topic中序列化云端事件时出错 %s: %s", pubSubName, msg.Topic, err)
						return err
					}
				} else {
					err = a.json.Unmarshal(msg.Data, &cloudEvent) // 反序列化
					if err != nil {
						log.Errorf("在pubsub %s和topic中序列化云端事件时出错%s: %s", pubSubName, msg.Topic, err)
						return err
					}
				}

				if pubsub.HasExpired(cloudEvent) {
					log.Warnf("丢弃过期的pub/sub事件 %v as of %v", cloudEvent[pubsub.IDField], cloudEvent[pubsub.ExpirationField])
					return nil
				}

				route := a.topicRoutes[pubSubName].routes[msg.Topic]
				routePath, shouldProcess, err := findMatchingRoute(&route, cloudEvent, a.featureRoutingEnabled) // 路由,消息 false
				if err != nil {
					return err
				}
				if !shouldProcess {
					// 该事件不符合指定的任何路线，所以忽略它。
					log.Debugf("没有匹配的事件路径 %v in pubsub %s and topic %s; skipping", cloudEvent[pubsub.IDField], pubSubName, msg.Topic)
					return nil
				}

				return publishFunc(ctx, &pubsubSubscribedMessage{
					cloudEvent: cloudEvent,
					data:       data,
					topic:      msg.Topic,
					metadata:   msg.Metadata,
					path:       routePath,
				})
			})
		if err != nil {
			log.Errorf("订阅主题失败 %s: %s", topic, err)
		}
	}

	return nil
}

// Publish 是一个适配器方法，用于运行时预验证发布请求 然后将它们转发给Pub/Sub组件。这个方法被HTTP和gRPC APIs使用。
func (a *DaprRuntime) Publish(req *pubsub.PublishRequest) error {
	thepubsub := a.GetPubSub(req.PubsubName)
	if thepubsub == nil {
		return runtime_pubsub.NotFoundError{PubsubName: req.PubsubName}
	}

	if allowed := a.isPubSubOperationAllowed(req.PubsubName, req.Topic, a.scopedPublishings[req.PubsubName]); !allowed {
		return runtime_pubsub.NotAllowedError{Topic: req.Topic, ID: a.runtimeConfig.ID}
	}

	return a.pubSubs[req.PubsubName].Publish(req)
}

// GetPubSub 是一个适配器方法，可以通过名字找到一个pubsub。
func (a *DaprRuntime) GetPubSub(pubsubName string) pubsub.PubSub {
	return a.pubSubs[pubsubName]
}

func (a *DaprRuntime) publishMessageHTTP(ctx context.Context, msg *pubsubSubscribedMessage) error {
	cloudEvent := msg.cloudEvent

	var span *trace.Span

	req := invokev1.NewInvokeMethodRequest(msg.path)
	req.WithHTTPExtension(nethttp.MethodPost, "")
	req.WithRawData(msg.data, contenttype.CloudEventContentType)
	req.WithCustomHTTPMetadata(msg.metadata)

	if cloudEvent[pubsub.TraceIDField] != nil {
		traceID := cloudEvent[pubsub.TraceIDField].(string)
		sc, _ := diag.SpanContextFromW3CString(traceID)
		spanName := fmt.Sprintf("pubsub/%s", msg.topic)
		ctx, span = diag.StartInternalCallbackSpan(ctx, spanName, sc, a.globalConfig.Spec.TracingSpec)
	}

	resp, err := a.appChannel.InvokeMethod(ctx, req)
	if err != nil {
		return errors.Wrap(err, "向应用程序发送pub/sub事件时，来自应用程序通道的错误")
	}

	statusCode := int(resp.Status().Code)

	if span != nil {
		m := diag.ConstructSubscriptionSpanAttributes(msg.topic)
		diag.AddAttributesToSpan(span, m)
		diag.UpdateSpanStatusFromHTTPStatus(span, statusCode)
		span.End()
	}

	_, body := resp.RawData()

	if (statusCode >= 200) && (statusCode <= 299) {
		// Any 2xx 被认为是成功的.
		var appResponse pubsub.AppResponse
		err := a.json.Unmarshal(body, &appResponse)
		if err != nil {
			log.Debugf("由于反序列化pub/sub事件的结果出错而跳过状态检查 %v", cloudEvent[pubsub.IDField])
			// 不返回错误，所以信息不会被重新处理。
			return nil // nolint:nilerr
		}

		switch appResponse.Status {
		case "":
			// 将空状态字段视为成功
			fallthrough
		//	Success AppResponseStatus = "SUCCESS"
		//	Retry AppResponseStatus = "RETRY"
		//	Drop AppResponseStatus = "DROP"
		case pubsub.Success:
			return nil
		case pubsub.Retry:
			return errors.Errorf("RETRY 处理pub/sub事件时从应用程序返回的状态 %v", cloudEvent[pubsub.IDField])
		case pubsub.Drop:
			log.Warnf("DROP 处理pub/sub事件时从应用程序返回的状态 %v", cloudEvent[pubsub.IDField])
			return nil
		}
		// 将未知状态字段视为错误并重试
		return errors.Errorf("在处理pub/sub事件时从应用程序返回的未知状态 %v: %v", cloudEvent[pubsub.IDField], appResponse.Status)
	}

	if statusCode == nethttp.StatusNotFound {
		// 这些是不可重试的错误，目前只是404，但可以添加更多的状态代码。
		// 当在这里添加/删除一个错误时，请检查该错误是否也适用于GRPC，因为在HTTP和GRPC错误之间有一个映射。
		// https://cloud.google.com/apis/design/errors#handling_errors
		log.Errorf("在处理pub/sub事件时，从应用程序返回的不可逆转的错误 %v:%s.返回的状态代码：%v", cloudEvent[pubsub.IDField], body, statusCode)
		return nil
	}

	// 从现在开始，每一个错误都是可修复的错误。
	log.Warnf("在处理pub/sub事件时，应用程序返回的可检索错误%v, topic: 返回的状态代码：%v。", cloudEvent[pubsub.IDField], cloudEvent[pubsub.TopicField], body, statusCode)
	return errors.Errorf("在处理pub/sub事件时从应用程序返回的可检索错误 %v, topic: %v, body: %s. : %v", cloudEvent[pubsub.IDField], cloudEvent[pubsub.TopicField], body, statusCode)
}

func (a *DaprRuntime) publishMessageGRPC(ctx context.Context, msg *pubsubSubscribedMessage) error {
	cloudEvent := msg.cloudEvent

	envelope := &runtimev1pb.TopicEventRequest{
		Id:              extractCloudEventProperty(cloudEvent, pubsub.IDField),
		Source:          extractCloudEventProperty(cloudEvent, pubsub.SourceField),
		DataContentType: extractCloudEventProperty(cloudEvent, pubsub.DataContentTypeField),
		Type:            extractCloudEventProperty(cloudEvent, pubsub.TypeField),
		SpecVersion:     extractCloudEventProperty(cloudEvent, pubsub.SpecVersionField),
		Topic:           msg.topic,
		PubsubName:      msg.metadata[pubsubName],
		Path:            msg.path,
	}

	if data, ok := cloudEvent[pubsub.DataBase64Field]; ok && data != nil {
		if dataAsString, ok := data.(string); ok {
			decoded, decodeErr := base64.StdEncoding.DecodeString(dataAsString)
			if decodeErr != nil {
				log.Debugf("unable to base64 decode cloudEvent field data_base64: %s", decodeErr)

				return decodeErr
			}

			envelope.Data = decoded
		} else {
			return ErrUnexpectedEnvelopeData
		}
	} else if data, ok := cloudEvent[pubsub.DataField]; ok && data != nil {
		envelope.Data = nil

		if contenttype.IsStringContentType(envelope.DataContentType) {
			switch v := data.(type) {
			case string:
				envelope.Data = []byte(v)
			case []byte:
				envelope.Data = v
			default:
				return ErrUnexpectedEnvelopeData
			}
		} else if contenttype.IsJSONContentType(envelope.DataContentType) {
			envelope.Data, _ = a.json.Marshal(data)
		}
	}

	var span *trace.Span
	if iTraceID, ok := cloudEvent[pubsub.TraceIDField]; ok {
		if traceID, ok := iTraceID.(string); ok {
			sc, _ := diag.SpanContextFromW3CString(traceID)
			spanName := fmt.Sprintf("pubsub/%s", msg.topic)

			// no ops if trace is off
			ctx, span = diag.StartInternalCallbackSpan(ctx, spanName, sc, a.globalConfig.Spec.TracingSpec)
			ctx = diag.SpanContextToGRPCMetadata(ctx, span.SpanContext())
		} else {
			log.Warnf("ignored non-string traceid value: %v", iTraceID)
		}
	}

	ctx = invokev1.WithCustomGRPCMetadata(ctx, msg.metadata)

	// call appcallback
	clientV1 := runtimev1pb.NewAppCallbackClient(a.grpc.AppClient)
	res, err := clientV1.OnTopicEvent(ctx, envelope)

	if span != nil {
		m := diag.ConstructSubscriptionSpanAttributes(envelope.Topic)
		diag.AddAttributesToSpan(span, m)
		diag.UpdateSpanStatusFromGRPCError(span, err)
		span.End()
	}

	if err != nil {
		errStatus, hasErrStatus := status.FromError(err)
		if hasErrStatus && (errStatus.Code() == codes.Unimplemented) {
			// DROP
			log.Warnf("non-retriable 处理pub/sub事件时从应用程序返回的状态%v: %s", cloudEvent[pubsub.IDField], err)

			return nil
		}

		err = errors.Errorf("处理pub/sub事件时从应用程序返回的状态%v: %s", cloudEvent[pubsub.IDField], err)
		log.Debug(err)

		// on error from application, return error for redelivery of event
		return err
	}

	switch res.GetStatus() {
	case runtimev1pb.TopicEventResponse_SUCCESS:
		// on uninitialized status, this is the case it defaults to as an uninitialized status defaults to 0 which is
		// success from protobuf definition
		return nil
	case runtimev1pb.TopicEventResponse_RETRY:
		return errors.Errorf("RETRY status returned from app while processing pub/sub event %v", cloudEvent[pubsub.IDField])
	case runtimev1pb.TopicEventResponse_DROP:
		log.Warnf("DROP status returned from app while processing pub/sub event %v", cloudEvent[pubsub.IDField])

		return nil
	}

	// Consider unknown status field as error and retry
	return errors.Errorf("unknown status returned from app while processing pub/sub event %v: %v", cloudEvent[pubsub.IDField], res.GetStatus())
}
func (a *DaprRuntime) startSubscribing() {
	for name, pubsub := range a.pubSubs {
		if err := a.beginPubSub(name, pubsub); err != nil {
			log.Errorf("开始pubsub时发生错误 %s: %s", name, err)
		}
	}
}

//获取声明式订阅
func (a *DaprRuntime) getDeclarativeSubscriptions() []runtime_pubsub.Subscription {
	var subs []runtime_pubsub.Subscription

	switch a.runtimeConfig.Mode {
	case modes.KubernetesMode:
		subs = runtime_pubsub.DeclarativeKubernetes(a.operatorClient, log)
	case modes.StandaloneMode:
		subs = runtime_pubsub.DeclarativeSelfHosted(a.runtimeConfig.Standalone.ComponentsPath, log)
	}

	// 只返回该应用ID的有效订阅
	for i := len(subs) - 1; i >= 0; i-- {
		s := subs[i]
		if len(s.Scopes) == 0 {
			continue
		}

		found := false
		// 判断该组件的作用域是不是该应用ID
		for _, scope := range s.Scopes {
			if scope == a.runtimeConfig.ID {
				found = true
				break
			}
		}

		if !found {
			subs = append(subs[:i], subs[i+1:]...)
		}
	}
	return subs
}

func (a *DaprRuntime) getTopicRoutes() (map[string]TopicRoute, error) {
	if a.topicRoutes != nil {
		return a.topicRoutes, nil
	}

	topicRoutes := make(map[string]TopicRoute)

	if a.appChannel == nil {
		log.Warn("应用程序通道未被初始化， 如果需要订阅pubsub，请确保 -app-port被指定")
		return topicRoutes, nil
	}

	var subscriptions []runtime_pubsub.Subscription
	var err error

	// 处理应用程序的订阅配置
	if a.runtimeConfig.ApplicationProtocol == HTTPProtocol {
		subscriptions, err = runtime_pubsub.GetSubscriptionsHTTP(a.appChannel, log)
	} else if a.runtimeConfig.ApplicationProtocol == GRPCProtocol {
		client := runtimev1pb.NewAppCallbackClient(a.grpc.AppClient)
		subscriptions, err = runtime_pubsub.GetSubscriptionsGRPC(client, log)
	}
	if err != nil {
		return nil, err
	}

	// 从k8s、本地文件夹获取所有订阅组件
	ds := a.getDeclarativeSubscriptions()
	for _, s := range ds {
		skip := false

		// 不要注册重复的订阅
		for _, sub := range subscriptions {
			if sub.PubsubName == s.PubsubName && sub.Topic == s.Topic {
				log.Warnf("发现两个相同的订阅 . pubsubname: %s, topic: %s", s.PubsubName, s.Topic)
				skip = true
				break
			}
		}

		if !skip {
			subscriptions = append(subscriptions, s)
		}
	}

	for _, s := range subscriptions {
		if _, ok := topicRoutes[s.PubsubName]; !ok {
			topicRoutes[s.PubsubName] = TopicRoute{routes: make(map[string]Route)}
		}

		topicRoutes[s.PubsubName].routes[s.Topic] = Route{metadata: s.Metadata, rules: s.Rules}
	}

	if len(topicRoutes) > 0 {
		for pubsubName, v := range topicRoutes {
			topics := []string{}
			for topic := range v.routes {
				topics = append(topics, topic)
			}
			log.Infof("应用程序订阅了以下主题: %v through pubsub=%s", topics, pubsubName)
		}
	}
	a.topicRoutes = topicRoutes
	return topicRoutes, nil
}

// 判断pubsub操作是否允许
func (a *DaprRuntime) isPubSubOperationAllowed(pubsubName string, topic string, scopedTopics []string) bool {
	inAllowedTopics := false

	//首先检查subscriptionScopes、publishingScopes是否包含它
	if len(a.allowedTopics[pubsubName]) > 0 {
		for _, t := range a.allowedTopics[pubsubName] {
			if t == topic {
				inAllowedTopics = true
				break
			}
		}
		if !inAllowedTopics {
			return false
		}
	}
	if len(scopedTopics) == 0 {
		return true
	}
	// 检查是否应用了粒度范围
	allowedScope := false
	for _, t := range scopedTopics {
		if t == topic {
			allowedScope = true
			break
		}
	}
	return allowedScope
}

func extractCloudEventProperty(cloudEvent map[string]interface{}, property string) string {
	if cloudEvent == nil {
		return ""
	}
	iValue, ok := cloudEvent[property]
	if ok {
		if value, ok := iValue.(string); ok {
			return value
		}
	}

	return ""
}
