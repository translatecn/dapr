package http

import (
	"github.com/dapr/dapr/pkg/messages"
	"github.com/valyala/fasthttp"
)

// 健康检查
func (a *api) constructHealthzEndpoints() []Endpoint {
	return []Endpoint{
		{
			Methods: []string{fasthttp.MethodGet},
			Route:   "healthz", //      curl http://localhost:3500/v1.0/healthz
			Version: apiVersionV1,
			Handler: a.onGetHealthz,
		},
		{
			Methods: []string{fasthttp.MethodGet},
			Route:   "healthz/outbound", // curl http://localhost:3500/v1.0/healthz/outbound
			Version: apiVersionV1,
			Handler: a.onGetOutboundHealthz,
		},
	}
}

//ok
func (a *api) onGetHealthz(reqCtx *fasthttp.RequestCtx) {
	if !a.readyStatus {
		msg := NewErrorResponse("ERR_HEALTH_NOT_READY", messages.ErrHealthNotReady)
		respond(reqCtx, withError(fasthttp.StatusInternalServerError, msg))
		log.Debug(msg)
	} else {
		respond(reqCtx, withEmpty())
	}
}

//ok
func (a *api) onGetOutboundHealthz(reqCtx *fasthttp.RequestCtx) {
	if !a.outboundReadyStatus {
		msg := NewErrorResponse("ERR_HEALTH_NOT_READY", messages.ErrHealthNotReady)
		respond(reqCtx, withError(fasthttp.StatusInternalServerError, msg))
		log.Debug(msg)
	} else {
		respond(reqCtx, withEmpty())
	}
}
