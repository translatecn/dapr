// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package http

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	cors "github.com/AdhityaRamadhanus/fasthttpcors"
	routing "github.com/fasthttp/router"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/pprofhandler"

	"github.com/dapr/dapr/pkg/config"
	cors_dapr "github.com/dapr/dapr/pkg/cors"
	diag "github.com/dapr/dapr/pkg/diagnostics"
	diag_utils "github.com/dapr/dapr/pkg/diagnostics/utils"
	http_middleware "github.com/dapr/dapr/pkg/middleware/http"
	auth "github.com/dapr/dapr/pkg/runtime/security"
	"github.com/dapr/kit/logger"
)

var log = logger.NewLogger("dapr.runtime.http")

const protocol = "http"

// Server dapr http 服务器接口
type Server interface {
	io.Closer
	StartNonBlocking() error
}

type server struct {
	config             ServerConfig             // 配置
	tracingSpec        config.TracingSpec       // 追踪配置
	metricSpec         config.MetricSpec        // 指标配置
	pipeline           http_middleware.Pipeline // 中间件配置
	api                API                      // 路由信息,等等
	apiSpec            config.APISpec           //  Dapr API的配置信息
	servers            []*fasthttp.Server       // fasthttp 服务
	profilingListeners []net.Listener           // 性能分析,在启动时添加
}

// NewServer 返回  HTTP 服务.
func NewServer(api API, config ServerConfig, tracingSpec config.TracingSpec, metricSpec config.MetricSpec, pipeline http_middleware.Pipeline, apiSpec config.APISpec) Server {
	return &server{
		api:         api,
		config:      config,
		tracingSpec: tracingSpec,
		metricSpec:  metricSpec,
		pipeline:    pipeline,
		apiSpec:     apiSpec,
	}
}

// StartNonBlocking 在goroutine. 中启动服务
func (s *server) StartNonBlocking() error {
	// ➕中间件
	handler := useAPIAuthentication(s.useCors(s.useComponents(s.useRouter())))
	handler = s.useMetrics(handler)
	handler = s.useTracing(handler)

	var listeners []net.Listener
	var profilingListeners []net.Listener
	if s.config.UnixDomainSocket != "" {
		socket := fmt.Sprintf("%s/dapr-%s-http.socket", s.config.UnixDomainSocket, s.config.AppID)
		l, err := net.Listen("unix", socket)
		if err != nil {
			return err
		}
		listeners = append(listeners, l)
	} else {
		for _, apiListenAddress := range s.config.APIListenAddresses {
			l, err := net.Listen("tcp", fmt.Sprintf("%s:%v", apiListenAddress, s.config.Port))
			if err != nil {
				log.Warnf("Failed to listen on %v:%v with error: %v", apiListenAddress, s.config.Port, err)
			} else {
				listeners = append(listeners, l)
			}
		}
	}
	if len(listeners) == 0 {
		return errors.Errorf("could not listen on any endpoint")
	}

	for _, listener := range listeners {
		// customServer is created in a loop because each instance
		// has a handle on the underlying listener.
		customServer := &fasthttp.Server{
			Handler:            handler,
			MaxRequestBodySize: s.config.MaxRequestBodySize * 1024 * 1024,
			ReadBufferSize:     s.config.ReadBufferSize * 1024,
			StreamRequestBody:  s.config.StreamRequestBody,
		}
		s.servers = append(s.servers, customServer)

		go func(l net.Listener) {
			if err := customServer.Serve(l); err != nil {
				log.Fatal(err)
			}
		}(listener)
	}

	if s.config.PublicPort != nil {
		publicHandler := s.usePublicRouter()
		publicHandler = s.useMetrics(publicHandler)
		publicHandler = s.useTracing(publicHandler)

		healthServer := &fasthttp.Server{
			Handler:            publicHandler,
			MaxRequestBodySize: s.config.MaxRequestBodySize * 1024 * 1024,
		}
		s.servers = append(s.servers, healthServer)

		go func() {
			if err := healthServer.ListenAndServe(fmt.Sprintf(":%d", *s.config.PublicPort)); err != nil {
				log.Fatal(err)
			}
		}()
	}
	// 如果允许性能分析，
	if s.config.EnableProfiling {
		for _, apiListenAddress := range s.config.APIListenAddresses {
			log.Infof("starting profiling server on %v:%v", apiListenAddress, s.config.ProfilePort)
			pl, err := net.Listen("tcp", fmt.Sprintf("%s:%v", apiListenAddress, s.config.ProfilePort))
			if err != nil {
				log.Warnf("Failed to listen on %v:%v with error: %v", apiListenAddress, s.config.ProfilePort, err)
			} else {
				profilingListeners = append(profilingListeners, pl)
			}
		}

		if len(profilingListeners) == 0 {
			return errors.Errorf("could not listen on any endpoint for profiling API")
		}

		s.profilingListeners = profilingListeners
		for _, listener := range profilingListeners {
			// profServer is created in a loop because each instance
			// has a handle on the underlying listener.
			profServer := &fasthttp.Server{
				Handler:            pprofhandler.PprofHandler,
				MaxRequestBodySize: s.config.MaxRequestBodySize * 1024 * 1024,
			}
			s.servers = append(s.servers, profServer)

			go func(l net.Listener) {
				if err := profServer.Serve(l); err != nil {
					log.Fatal(err)
				}
			}(listener)
		}
	}

	return nil
}

func (s *server) Close() error {
	var merr error

	for _, ln := range s.servers {
		// This calls `Close()` on the underlying listener.
		if err := ln.Shutdown(); err != nil {
			merr = multierror.Append(merr, err)
		}
	}

	return merr
}

// 追踪middleware
func (s *server) useTracing(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	if diag_utils.IsTracingEnabled(s.tracingSpec.SamplingRate) {
		log.Infof("enabled tracing http middleware")
		return diag.HTTPTraceMiddleware(next, s.config.AppID, s.tracingSpec)
	}
	return next
}

// 指标middleware
func (s *server) useMetrics(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	if s.metricSpec.Enabled {
		log.Infof("enabled metrics http middleware")

		return diag.DefaultHTTPMonitoring.FastHTTPMiddleware(next)
	}

	return next
}

// 获取所有路由
func (s *server) useRouter() fasthttp.RequestHandler {
	endpoints := s.api.APIEndpoints()
	router := s.getRouter(endpoints)

	return router.Handler
}

// middleware
func (s *server) usePublicRouter() fasthttp.RequestHandler {
	endpoints := s.api.PublicEndpoints()
	router := s.getRouter(endpoints)

	return router.Handler
}

// middleware
func (s *server) useComponents(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	return s.pipeline.Apply(next)
}

// middleware
func (s *server) useCors(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	if s.config.AllowedOrigins == cors_dapr.DefaultAllowedOrigins {
		return next
	}

	log.Infof("启用Cors http中间件")
	origins := strings.Split(s.config.AllowedOrigins, ",")
	corsHandler := s.getCorsHandler(origins)
	return corsHandler.CorsMiddleware(next)
}

// middleware
func useAPIAuthentication(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	token := auth.GetAPIToken()
	if token == "" {
		return next
	}
	log.Info("启用了http服务器上的令牌认证")

	return func(ctx *fasthttp.RequestCtx) {
		v := ctx.Request.Header.Peek(auth.APITokenHeader)
		if auth.ExcludedRoute(string(ctx.Request.URI().FullURI())) || string(v) == token {
			ctx.Request.Header.Del(auth.APITokenHeader)
			next(ctx)
		} else {
			ctx.Error("invalid api token", http.StatusUnauthorized)
		}
	}
}

func (s *server) getCorsHandler(allowedOrigins []string) *cors.CorsHandler {
	return cors.NewCorsHandler(cors.Options{
		AllowedOrigins: allowedOrigins,
		Debug:          false,
	})
}

// 编码URL
func (s *server) unescapeRequestParametersHandler(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		parseError := false
		unescapeRequestParameters := func(parameter []byte, value interface{}) {
			switch value.(type) {
			case string:
				if !parseError {
					parameterValue := fmt.Sprintf("%v", value)
					//go http请求如果参数中带有"等特殊字符，参数传输可能会出现问题，所以传输前需要进行参数编码。
					parameterUnescapedValue, err := url.QueryUnescape(parameterValue)
					if err == nil {
						ctx.SetUserValueBytes(parameter, parameterUnescapedValue)
					} else {
						parseError = true
						errorMessage := fmt.Sprintf("Failed to unescape request parameter %s with value %v. Error: %s", parameter, value, err.Error())
						log.Debug(errorMessage)
						ctx.Error(errorMessage, fasthttp.StatusBadRequest)
					}
				}
			}
		}
		ctx.VisitUserValues(unescapeRequestParameters)

		if !parseError {
			next(ctx)
		}
	}
}

// 路由
func (s *server) getRouter(endpoints []Endpoint) *routing.Router {
	router := routing.New()
	//	{
	//			Methods: []string{router.MethodWild},
	//			Route:   "invoke/{id}/method/{method:*}",
	//			Alias:   "{method:*}",
	//			Version: apiVersionV1,     "v1.0"
	//			Handler: a.onDirectMessage,
	//		},
	parameterFinder, _ := regexp.Compile("/{.*}")
	for _, e := range endpoints {
		if !s.endpointAllowed(e) {
			continue
		}
		//eg  dapr_url = f"/v1.0/invoke/{app id}/method/api/v1/executor/"
		path := fmt.Sprintf("/%s/%s", e.Version, e.Route)
		s.handle(e, parameterFinder, path, router)
		if e.Alias != "" {
			path = fmt.Sprintf("/%s", e.Alias)
			s.handle(e, parameterFinder, path, router)
		}
	}

	for k, v := range router.List() {
		for _, path := range v {
			log.Infof("%v----%v\n", k, path)
		}
	}
	return router
}

func (s *server) handle(e Endpoint, parameterFinder *regexp.Regexp, path string, router *routing.Router) {
	for _, m := range e.Methods {
		// 判断待匹配的字符串是不是  /{.*}
		pathIncludesParameters := parameterFinder.MatchString(path)
		// 添加路由
		if pathIncludesParameters {
			// 编码url
			router.Handle(m, path, s.unescapeRequestParametersHandler(e.Handler))
		} else {
			router.Handle(m, path, e.Handler)
		}
	}
}

// 是否允许该端点
func (s *server) endpointAllowed(endpoint Endpoint) bool {
	var httpRules []config.APIAccessRule

	// 允许的http路由
	for _, rule := range s.apiSpec.Allowed {
		if rule.Protocol == protocol {
			httpRules = append(httpRules, rule)
		}
	}
	if len(httpRules) == 0 {
		return true
	}

	for _, rule := range httpRules {
		//  0 代表endpoint.Route 以rule.Name 开头
		if (strings.Index(endpoint.Route, rule.Name) == 0 && endpoint.Version == rule.Version) || endpoint.Route == "healthz" {
			return true
		}
	}

	return false
}
