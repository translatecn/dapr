// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package http

import (
	"encoding/json"

	jsoniter "github.com/json-iterator/go"
	"github.com/valyala/fasthttp"
)

const (
	jsonContentTypeHeader = "application/json"
	etagHeader            = "ETag"
	metadataPrefix        = "metadata."
)

// BulkGetResponse 是一个状态批量获取操作的响应对象。
type BulkGetResponse struct {
	Key      string              `json:"key"`
	Data     jsoniter.RawMessage `json:"data,omitempty"`
	ETag     *string             `json:"etag,omitempty"`
	Metadata map[string]string   `json:"metadata,omitempty"`
	Error    string              `json:"error,omitempty"`
}

// QueryResponse 是用于查询状态的响应对象。
type QueryResponse struct {
	Results  []QueryItem       `json:"results"`
	Token    string            `json:"token,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// QueryItem 是一个代表查询结果中单个条目的对象。
type QueryItem struct {
	Key   string              `json:"key"`
	Data  jsoniter.RawMessage `json:"data"`
	ETag  *string             `json:"etag,omitempty"`
	Error string              `json:"error,omitempty"`
}

type option = func(ctx *fasthttp.RequestCtx)

// withEtag 设置 etag header.
func withEtag(etag *string) option {
	return func(ctx *fasthttp.RequestCtx) {
		if etag != nil {
			ctx.Response.Header.Set(etagHeader, *etag)
		}
	}
}

// withMetadata 使用metadata设置header
func withMetadata(metadata map[string]string) option {
	return func(ctx *fasthttp.RequestCtx) {
		for k, v := range metadata {
			ctx.Response.Header.Set(metadataPrefix+k, v)
		}
	}
}

// withJSON 用application/json重写内容类型。
func withJSON(code int, obj []byte) option {
	return func(ctx *fasthttp.RequestCtx) {
		ctx.Response.SetStatusCode(code)
		ctx.Response.SetBody(obj)
		ctx.Response.Header.SetContentType(jsonContentTypeHeader)
	}
}

// withError 设置错误代码和json化的错误信息。
func withError(code int, resp ErrorResponse) option {
	b, _ := json.Marshal(&resp)
	return withJSON(code, b)
}

// withEmpty 设置204状态码
func withEmpty() option {
	return func(ctx *fasthttp.RequestCtx) {
		ctx.Response.SetBody(nil)
		ctx.Response.SetStatusCode(fasthttp.StatusNoContent)
	}
}

// with 如果内容类型不存在，则设置一个默认的application/json内容类型。
func with(code int, obj []byte) option {
	return func(ctx *fasthttp.RequestCtx) {
		ctx.Response.SetStatusCode(code)
		ctx.Response.SetBody(obj)

		if len(ctx.Response.Header.ContentType()) == 0 {
			ctx.Response.Header.SetContentType(jsonContentTypeHeader)
		}
	}
}

func respond(ctx *fasthttp.RequestCtx, options ...option) {
	for _, option := range options {
		option(ctx)
	}
}
