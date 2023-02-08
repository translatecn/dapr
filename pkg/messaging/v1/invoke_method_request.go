// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package v1

import (
	"errors"
	"strings"

	"github.com/valyala/fasthttp"
	"google.golang.org/protobuf/types/known/anypb"

	commonv1pb "github.com/dapr/dapr/pkg/proto/common/v1"
	internalv1pb "github.com/dapr/dapr/pkg/proto/internals/v1"
)

const (
	// DefaultAPIVersion dapr api 版本
	DefaultAPIVersion = internalv1pb.APIVersion_V1
)

// InvokeMethodRequest 持有 InternalInvokeRequest protobuf消息，并提供帮助器来管理它。
type InvokeMethodRequest struct {
	r *internalv1pb.InternalInvokeRequest
}

// NewInvokeMethodRequest 为方法创建InvokeMethodRequest对象。
func NewInvokeMethodRequest(method string) *InvokeMethodRequest {
	return &InvokeMethodRequest{
		r: &internalv1pb.InternalInvokeRequest{
			Ver: DefaultAPIVersion,
			Message: &commonv1pb.InvokeRequest{
				Method: method, // 被调用法的方法,具体的执行逻辑
			},
		},
	}
}

// FromInvokeRequestMessage creates InvokeMethodRequest object from InvokeRequest pb object.
func FromInvokeRequestMessage(pb *commonv1pb.InvokeRequest) *InvokeMethodRequest {
	return &InvokeMethodRequest{
		r: &internalv1pb.InternalInvokeRequest{
			Ver:     DefaultAPIVersion,
			Message: pb,
		},
	}
}

// InternalInvokeRequest creates InvokeMethodRequest object from InternalInvokeRequest pb object.
func InternalInvokeRequest(pb *internalv1pb.InternalInvokeRequest) (*InvokeMethodRequest, error) {
	req := &InvokeMethodRequest{r: pb}
	if pb.Message == nil {
		return nil, errors.New("Message field is nil")
	}

	return req, nil
}

// WithActor sets actor type and id.
func (imr *InvokeMethodRequest) WithActor(actorType, actorID string) *InvokeMethodRequest {
	imr.r.Actor = &internalv1pb.Actor{ActorType: actorType, ActorId: actorID}
	return imr
}

// WithMetadata sets metadata.
func (imr *InvokeMethodRequest) WithMetadata(md map[string][]string) *InvokeMethodRequest {
	imr.r.Metadata = MetadataToInternalMetadata(md)
	return imr
}

// WithFastHTTPHeaders 将fast http 请求头 封装到内部的请求中
func (imr *InvokeMethodRequest) WithFastHTTPHeaders(header *fasthttp.RequestHeader) *InvokeMethodRequest {
	md := map[string][]string{}
	header.VisitAll(func(key []byte, value []byte) {
		md[string(key)] = []string{string(value)}
	})
	imr.r.Metadata = MetadataToInternalMetadata(md)
	return imr
}

// WithRawData 设置消息的格式
func (imr *InvokeMethodRequest) WithRawData(data []byte, contentType string) *InvokeMethodRequest {
	if contentType == "" {
		contentType = JSONContentType
	}
	imr.r.Message.ContentType = contentType
	imr.r.Message.Data = &anypb.Any{Value: data}
	return imr
}

// WithHTTPExtension 设置http的请求方式和查询字符串
func (imr *InvokeMethodRequest) WithHTTPExtension(verb string, querystring string) *InvokeMethodRequest {
	httpMethod, ok := commonv1pb.HTTPExtension_Verb_value[strings.ToUpper(verb)]
	if !ok {
		// 如果请求方式不存在，就改成post
		httpMethod = int32(commonv1pb.HTTPExtension_POST)
	}
	imr.r.Message.HttpExtension = &commonv1pb.HTTPExtension{
		Verb:        commonv1pb.HTTPExtension_Verb(httpMethod),
		Querystring: querystring,
	}

	return imr
}

// WithCustomHTTPMetadata applies a metadata map to a InvokeMethodRequest.
func (imr *InvokeMethodRequest) WithCustomHTTPMetadata(md map[string]string) *InvokeMethodRequest {
	for k, v := range md {
		if imr.r.Metadata == nil {
			imr.r.Metadata = make(map[string]*internalv1pb.ListStringValue)
		}

		// NOTE: We don't explicitly lowercase the keys here but this will be done
		//       later when attached to the HTTP request as headers.
		imr.r.Metadata[k] = &internalv1pb.ListStringValue{Values: []string{v}}
	}

	return imr
}

// EncodeHTTPQueryString generates querystring for http using http extension object.
func (imr *InvokeMethodRequest) EncodeHTTPQueryString() string {
	m := imr.r.Message
	if m == nil || m.GetHttpExtension() == nil {
		return ""
	}

	return m.GetHttpExtension().Querystring
}

// APIVersion 返回请求调用的 版本
func (imr *InvokeMethodRequest) APIVersion() internalv1pb.APIVersion {
	return imr.r.GetVer()
}

// Metadata 返回 InvokeMethodRequest 的Metadata 字段
func (imr *InvokeMethodRequest) Metadata() DaprInternalMetadata {
	return imr.r.GetMetadata()
}

// Proto 返回 InternalInvokeRequest Proto 对象。
func (imr *InvokeMethodRequest) Proto() *internalv1pb.InternalInvokeRequest {
	return imr.r
}

// Actor 返回actor类型和id。
func (imr *InvokeMethodRequest) Actor() *internalv1pb.Actor {
	return imr.r.GetActor()
}

// Message 返回调用请求的消息体
func (imr *InvokeMethodRequest) Message() *commonv1pb.InvokeRequest {
	return imr.r.Message
}

// RawData 返回content_type和byte array body。
func (imr *InvokeMethodRequest) RawData() (string, []byte) {
	m := imr.r.Message
	if m == nil || m.Data == nil {
		return "", nil
	}

	contentType := m.GetContentType()       // application/json
	dataTypeURL := m.GetData().GetTypeUrl() // ""
	dataValue := m.GetData().GetValue()     // {"a": 800.2914473430634}

	// 仅当typeurl未设置且数据已给定时，将content_type设置为application/json。
	if contentType == "" && dataTypeURL == "" && dataValue != nil {
		contentType = JSONContentType
	}

	return contentType, dataValue
}

// AddHeaders Adds a new header to the existing set.
func (imr *InvokeMethodRequest) AddHeaders(header *fasthttp.RequestHeader) {
	md := map[string][]string{}
	header.VisitAll(func(key []byte, value []byte) {
		md[string(key)] = []string{string(value)}
	})

	internalMd := MetadataToInternalMetadata(md)

	if imr.r.Metadata == nil {
		imr.r.Metadata = internalMd
	} else {
		for key, val := range internalMd {
			// We're only adding new values, not overwriting existing
			if _, ok := imr.r.Metadata[key]; !ok {
				imr.r.Metadata[key] = val
			}
		}
	}
}
