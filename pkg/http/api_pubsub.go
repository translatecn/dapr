package http

import (
	"fmt"
	contrib_metadata "github.com/dapr/components-contrib/metadata"
	"github.com/dapr/components-contrib/pubsub"
	diag "github.com/dapr/dapr/pkg/diagnostics"
	diag_utils "github.com/dapr/dapr/pkg/diagnostics/utils"
	"github.com/dapr/dapr/pkg/messages"
	runtime_pubsub "github.com/dapr/dapr/pkg/runtime/pubsub"
	"github.com/pkg/errors"
	"github.com/valyala/fasthttp"
)

// 构建pubsub端点
func (a *api) constructPubSubEndpoints() []Endpoint {
	return []Endpoint{
		{
			Methods: []string{fasthttp.MethodPost, fasthttp.MethodPut},
			Route:   "publish/{pubsubname}/{topic:*}",
			Version: apiVersionV1,
			Handler: a.onPublish,
		},
	}
}

func (a *api) onPublish(reqCtx *fasthttp.RequestCtx) {
	if a.pubsubAdapter == nil {
		msg := NewErrorResponse("ERR_PUBSUB_NOT_CONFIGURED", messages.ErrPubsubNotConfigured)
		respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
		log.Debug(msg)

		return
	}
	// 获取组件的名字
	pubsubName := reqCtx.UserValue(pubsubnameparam).(string)
	if pubsubName == "" {
		msg := NewErrorResponse("ERR_PUBSUB_EMPTY", messages.ErrPubsubEmpty)
		respond(reqCtx, withError(fasthttp.StatusNotFound, msg))
		log.Debug(msg)

		return
	}

	thepubsub := a.pubsubAdapter.GetPubSub(pubsubName)
	if thepubsub == nil {
		msg := NewErrorResponse("ERR_PUBSUB_NOT_FOUND", fmt.Sprintf(messages.ErrPubsubNotFound, pubsubName))
		respond(reqCtx, withError(fasthttp.StatusNotFound, msg))
		log.Debug(msg)

		return
	}
	// 获取主题的名称
	topic := reqCtx.UserValue(topicParam).(string)
	if topic == "" {
		msg := NewErrorResponse("ERR_TOPIC_EMPTY", fmt.Sprintf(messages.ErrTopicEmpty, pubsubName))
		respond(reqCtx, withError(fasthttp.StatusNotFound, msg))
		log.Debug(msg)

		return
	}
	// 用户提交的数据
	body := reqCtx.PostBody()
	contentType := string(reqCtx.Request.Header.Peek("Content-Type"))
	metadata := getMetadataFromRequest(reqCtx)                     // header前缀是metadata.
	rawPayload, metaErr := contrib_metadata.IsRawPayload(metadata) // key 为 rawPayload
	if metaErr != nil {
		msg := NewErrorResponse("ERR_PUBSUB_REQUEST_METADATA",
			fmt.Sprintf(messages.ErrMetadataGet, metaErr.Error()))
		respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
		log.Debug(msg)

		return
	}

	// 从上下文中提取跟踪上下文。
	span := diag_utils.SpanFromContext(reqCtx)
	// 填充W3C从tracparent到cloudevent的信封
	corID := diag.SpanContextToW3CString(span.SpanContext())

	data := body
	// 不是原始负载
	if !rawPayload {
		//信封
		envelope, err := runtime_pubsub.NewCloudEvent(&runtime_pubsub.CloudEvent{
			ID:              a.id, // 自己的应用ID
			Topic:           topic,
			DataContentType: contentType,
			Data:            body,
			TraceID:         corID,
			Pubsub:          pubsubName,
		})
		if err != nil {
			msg := NewErrorResponse("ERR_PUBSUB_CLOUD_EVENTS_SER",
				fmt.Sprintf(messages.ErrPubsubCloudEventCreation, err.Error()))
			respond(reqCtx, withError(fasthttp.StatusInternalServerError, msg))
			log.Debug(msg)

			return
		}
		//获取组件实现的一些特性
		features := thepubsub.Features()
		// 支持过期时间、就添加过期字段
		pubsub.ApplyMetadata(envelope, features, metadata)

		data, err = a.json.Marshal(envelope)
		if err != nil {
			msg := NewErrorResponse("ERR_PUBSUB_CLOUD_EVENTS_SER",
				fmt.Sprintf(messages.ErrPubsubCloudEventsSer, topic, pubsubName, err.Error()))
			respond(reqCtx, withError(fasthttp.StatusInternalServerError, msg))
			log.Debug(msg)

			return
		}
	}

	req := pubsub.PublishRequest{
		PubsubName: pubsubName, // 组件名称
		Topic:      topic,      // 主题名称
		Data:       data,       // 发送的数据
		Metadata:   metadata,   // 元数据
	}
	//{
	//		"pubsubname":"redis-pubsub",
	//		"traceid":"00-924ae7154e9e2894001b408b20cfa4c7-889e95bb1eca368b-00",
	//		"id":"0db251bc-c96a-4b2a-b967-301ed25d56d6",
	//		"type":"com.dapr.event.sent","source":"dp-61c2cb20562850d49d47d1c7-executorapp",
	//		"topic":"topic-c",
	//		"data":"{\"demo\": \"test\"}",
	//		"expiration":"2021-12-27T06:56:48Z",
	//		"specversion":"1.0",
	//		"datacontenttype":"text/plain"
	//}
	// 调用实际的组件
	//metaErr := thepubsub.Publish(&req)
	err := a.pubsubAdapter.Publish(&req)
	if err != nil {
		status := fasthttp.StatusInternalServerError
		msg := NewErrorResponse("ERR_PUBSUB_PUBLISH_MESSAGE",
			fmt.Sprintf(messages.ErrPubsubPublishMessage, topic, pubsubName, err.Error()))

		if errors.As(err, &runtime_pubsub.NotAllowedError{}) {
			msg = NewErrorResponse("ERR_PUBSUB_FORBIDDEN", err.Error())
			status = fasthttp.StatusForbidden
		}

		if errors.As(err, &runtime_pubsub.NotFoundError{}) {
			msg = NewErrorResponse("ERR_PUBSUB_NOT_FOUND", err.Error())
			status = fasthttp.StatusBadRequest
		}

		respond(reqCtx, withError(status, msg))
		log.Debug(msg)
	} else {
		respond(reqCtx, withEmpty())
	}
}
