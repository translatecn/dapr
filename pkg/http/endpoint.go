// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package http

import "github.com/valyala/fasthttp"

// Endpoint dapr api 路由信息的集合
type Endpoint struct {
	Methods []string
	Route   string
	Version string
	Alias   string
	Handler fasthttp.RequestHandler
}
