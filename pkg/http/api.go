// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package http

import (
	"encoding/base64"
	"strconv"
	"strings"
	"sync"

	"github.com/dapr/components-contrib/bindings"
	"github.com/dapr/components-contrib/secretstores"
	"github.com/dapr/components-contrib/state"
	"github.com/dapr/dapr/pkg/actors"
	components_v1alpha1 "github.com/dapr/dapr/pkg/apis/components/v1alpha1"
	"github.com/dapr/dapr/pkg/channel"
	"github.com/dapr/dapr/pkg/channel/http"
	"github.com/dapr/dapr/pkg/config"
	"github.com/dapr/dapr/pkg/messaging"
	runtime_pubsub "github.com/dapr/dapr/pkg/runtime/pubsub"
	jsoniter "github.com/json-iterator/go"
	"github.com/valyala/fasthttp"
)

// API 返回Dapr的HTTP端点的列表。
type API interface {
	APIEndpoints() []Endpoint
	PublicEndpoints() []Endpoint
	MarkStatusAsReady()
	MarkStatusAsOutboundReady()
	SetAppChannel(appChannel channel.AppChannel)
	SetDirectMessaging(directMessaging messaging.DirectMessaging)
	SetActorRuntime(actor actors.Actors)
}

type api struct {
	endpoints                []Endpoint
	publicEndpoints          []Endpoint
	directMessaging          messaging.DirectMessaging
	appChannel               channel.AppChannel
	getComponentsFn          func() []components_v1alpha1.Component
	stateStores              map[string]state.Store
	transactionalStateStores map[string]state.TransactionalStore
	secretStores             map[string]secretstores.SecretStore
	secretsConfiguration     map[string]config.SecretsScope
	json                     jsoniter.API
	actor                    actors.Actors
	pubsubAdapter            runtime_pubsub.Adapter
	sendToOutputBindingFn    func(name string, req *bindings.InvokeRequest) (*bindings.InvokeResponse, error)
	id                       string // 自己的应用ID
	extendedMetadata         sync.Map
	readyStatus              bool // 所有逻辑均准备后 ,置为true
	outboundReadyStatus      bool // http服务开启后，置为true
	tracingSpec              config.TracingSpec
	shutdown                 func()
}

type registeredComponent struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Version string `json:"version"`
}

type metadata struct {
	ID                   string                      `json:"id"`
	ActiveActorsCount    []actors.ActiveActorsCount  `json:"actors"`
	Extended             map[interface{}]interface{} `json:"extended"`
	RegisteredComponents []registeredComponent       `json:"components"`
}

const (
	apiVersionV1         = "v1.0"
	apiVersionV1alpha1   = "v1.0-alpha1"
	idParam              = "id"
	methodParam          = "method"
	topicParam           = "topic"
	actorTypeParam       = "actorType"
	actorIDParam         = "actorId"
	storeNameParam       = "storeName"
	stateKeyParam        = "key"
	secretStoreNameParam = "secretStoreName"
	secretNameParam      = "key"
	nameParam            = "name"
	consistencyParam     = "consistency" // 一致性
	concurrencyParam     = "concurrency" // 并发
	pubsubnameparam      = "pubsubname"
	traceparentHeader    = "traceparent"
	tracestateHeader     = "tracestate"
	daprAppID            = "dapr-app-id"
)

// NewAPI returns a new API.
// 无非就是将RuntimeConfig里的数据来回传,避免依赖
func NewAPI(
	appID string,
	appChannel channel.AppChannel,
	directMessaging messaging.DirectMessaging,
	getComponentsFn func() []components_v1alpha1.Component,
	stateStores map[string]state.Store,
	secretStores map[string]secretstores.SecretStore,
	secretsConfiguration map[string]config.SecretsScope,
	pubsubAdapter runtime_pubsub.Adapter,
	actor actors.Actors,
	sendToOutputBindingFn func(name string, req *bindings.InvokeRequest) (*bindings.InvokeResponse, error),
	tracingSpec config.TracingSpec,
	shutdown func()) API {
	transactionalStateStores := map[string]state.TransactionalStore{}
	for key, store := range stateStores {
		if state.FeatureTransactional.IsPresent(store.Features()) {
			transactionalStateStores[key] = store.(state.TransactionalStore)
		}
	}
	api := &api{
		appChannel:               appChannel,
		getComponentsFn:          getComponentsFn,
		directMessaging:          directMessaging,
		stateStores:              stateStores,
		transactionalStateStores: transactionalStateStores,
		secretStores:             secretStores,
		secretsConfiguration:     secretsConfiguration,
		json:                     jsoniter.ConfigFastest,
		actor:                    actor,
		pubsubAdapter:            pubsubAdapter,
		sendToOutputBindingFn:    sendToOutputBindingFn,
		id:                       appID,
		tracingSpec:              tracingSpec,
		shutdown:                 shutdown,
	}

	//	注册路由
	metadataEndpoints := api.constructMetadataEndpoints()
	healthEndpoints := api.constructHealthzEndpoints()

	api.endpoints = append(api.endpoints, api.constructStateEndpoints()...)
	api.endpoints = append(api.endpoints, api.constructSecretEndpoints()...)
	api.endpoints = append(api.endpoints, api.constructPubSubEndpoints()...)
	api.endpoints = append(api.endpoints, api.constructActorEndpoints()...)
	api.endpoints = append(api.endpoints, api.constructDirectMessagingEndpoints()...)
	api.endpoints = append(api.endpoints, metadataEndpoints...)
	api.endpoints = append(api.endpoints, api.constructShutdownEndpoints()...)
	api.endpoints = append(api.endpoints, api.constructBindingsEndpoints()...)
	api.endpoints = append(api.endpoints, healthEndpoints...)

	api.publicEndpoints = append(api.publicEndpoints, metadataEndpoints...)
	api.publicEndpoints = append(api.publicEndpoints, healthEndpoints...)

	return api
}

// APIEndpoints 返回注册的端点
func (a *api) APIEndpoints() []Endpoint {
	return a.endpoints
}

// PublicEndpoints 返回公开的端点
func (a *api) PublicEndpoints() []Endpoint {
	return a.publicEndpoints
}

// MarkStatusAsReady 标记dapr状态已就绪
func (a *api) MarkStatusAsReady() {
	a.readyStatus = true
}

// MarkStatusAsOutboundReady 标记dapr出站流量的已就绪。
func (a *api) MarkStatusAsOutboundReady() {
	a.outboundReadyStatus = true
}

// ----------------------------------------

// 停止
func (a *api) constructShutdownEndpoints() []Endpoint {
	return []Endpoint{
		{
			Methods: []string{fasthttp.MethodPost},
			Route:   "shutdown", //   curl -X POST http://localhost:3500/v1.0/shutdown
			Version: apiVersionV1,
			Handler: a.onShutdown,
		},
	}
}

// ----------------------------------------

// stateErrorResponse 接收一个状态存储错误，并返回相应的状态代码、错误信息和修改后的用户错误。
func (a *api) stateErrorResponse(err error, errorCode string) (int, string, ErrorResponse) {
	var message string
	var code int
	var etag bool
	etag, code, message = a.etagError(err)

	r := ErrorResponse{
		ErrorCode: errorCode,
	}
	if etag {
		return code, message, r
	}
	message = err.Error()

	return fasthttp.StatusInternalServerError, message, r
}

// findTargetID 获取请求中的目标应用ID
func (a *api) findTargetID(reqCtx *fasthttp.RequestCtx) string {
	// 1、url 中的 app id
	// 2、 header 中的 "dapr-app-id"
	// 3、 basic auth 			header 中的Authorization: Basic base64encode(username+":"+password)
	if id := reqCtx.UserValue(idParam); id == nil {
		if appID := reqCtx.Request.Header.Peek(daprAppID); appID == nil {
			if auth := reqCtx.Request.Header.Peek(fasthttp.HeaderAuthorization); auth != nil &&
				strings.HasPrefix(string(auth), "Basic ") {
				if s, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(string(auth), "Basic ")); err == nil {
					pair := strings.Split(string(s), ":")
					//
					if len(pair) == 2 && pair[0] == daprAppID {
						return pair[1]
					}
				}
			}
		} else {
			return string(appID)
		}
	} else {
		return id.(string)
	}

	return ""
}

func (a *api) onShutdown(reqCtx *fasthttp.RequestCtx) {
	if !reqCtx.IsPost() {
		log.Warn("Please use POST method when invoking shutdown API")
	}

	respond(reqCtx, withEmpty())
	go func() {
		a.shutdown()
	}()
}

// GetStatusCodeFromMetadata extracts the http status code from the metadata if it exists.
func GetStatusCodeFromMetadata(metadata map[string]string) int {
	code := metadata[http.HTTPStatusCode]
	if code != "" {
		statusCode, err := strconv.Atoi(code)
		if err == nil {
			return statusCode
		}
	}

	return fasthttp.StatusOK
}

func (a *api) isSecretAllowed(storeName, key string) bool {
	if config, ok := a.secretsConfiguration[storeName]; ok {
		return config.IsSecretAllowed(key)
	}
	// By default, if a configuration is not defined for a secret store, return true.
	return true
}

func (a *api) SetAppChannel(appChannel channel.AppChannel) {
	a.appChannel = appChannel
}
