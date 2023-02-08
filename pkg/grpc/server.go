// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package grpc

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/pkg/errors"
	grpc_go "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	"github.com/dapr/kit/logger"

	"github.com/dapr/dapr/pkg/config"
	diag "github.com/dapr/dapr/pkg/diagnostics"
	diag_utils "github.com/dapr/dapr/pkg/diagnostics/utils"
	"github.com/dapr/dapr/pkg/messaging"
	internalv1pb "github.com/dapr/dapr/pkg/proto/internals/v1"
	runtimev1pb "github.com/dapr/dapr/pkg/proto/runtime/v1"
	auth "github.com/dapr/dapr/pkg/runtime/security"
)

const (
	certWatchInterval              = time.Second * 3
	renewWhenPercentagePassed      = 70
	apiServer                      = "apiServer"
	internalServer                 = "internalServer"
	defaultMaxConnectionAgeSeconds = 30
)

// Server dapr grpc 服务器接口
type Server interface {
	io.Closer
	StartNonBlocking() error
}

type server struct {
	api                API
	config             ServerConfig
	tracingSpec        config.TracingSpec
	metricSpec         config.MetricSpec
	authenticator      auth.Authenticator
	servers            []*grpc_go.Server
	renewMutex         *sync.Mutex
	signedCert         *auth.SignedCertificate
	tlsCert            tls.Certificate
	signedCertDuration time.Duration // 证书有效期
	kind               string
	logger             logger.Logger
	maxConnectionAge   *time.Duration
	authToken          string
	apiSpec            config.APISpec
	proxy              messaging.Proxy
}

var (
	apiServerLogger      = logger.NewLogger("dapr.runtime.grpc.api")
	internalServerLogger = logger.NewLogger("dapr.runtime.grpc.internal")
)

// NewAPIServer 返回一个新的面向用户的gRPC API服务器。
func NewAPIServer(api API, config ServerConfig, tracingSpec config.TracingSpec, metricSpec config.MetricSpec, apiSpec config.APISpec, proxy messaging.Proxy) Server {
	return &server{
		api:         api,
		config:      config,
		tracingSpec: tracingSpec,
		metricSpec:  metricSpec,
		kind:        apiServer,
		logger:      apiServerLogger,
		authToken:   auth.GetAPIToken(),
		apiSpec:     apiSpec,
		proxy:       proxy,
	}
}

// NewInternalServer 返回一个dapr间通信的grpc服务端
func NewInternalServer(api API, config ServerConfig, tracingSpec config.TracingSpec, metricSpec config.MetricSpec, authenticator auth.Authenticator, proxy messaging.Proxy) Server {
	return &server{
		api:              api,
		config:           config,
		tracingSpec:      tracingSpec,
		metricSpec:       metricSpec,
		authenticator:    authenticator,
		renewMutex:       &sync.Mutex{},
		kind:             internalServer,
		logger:           internalServerLogger,
		maxConnectionAge: getDefaultMaxAgeDuration(),
		proxy:            proxy,
	}
}

func getDefaultMaxAgeDuration() *time.Duration {
	d := time.Second * defaultMaxConnectionAgeSeconds // 30秒
	return &d
}

// StartNonBlocking 在goroutine里启动服务
func (s *server) StartNonBlocking() error {
	var listeners []net.Listener
	if s.config.UnixDomainSocket != "" && s.kind == apiServer {
		socket := fmt.Sprintf("%s/dapr-%s-grpc.socket", s.config.UnixDomainSocket, s.config.AppID)
		l, err := net.Listen("unix", socket)
		if err != nil {
			return err
		}
		listeners = append(listeners, l)
	} else {
		// 在多个ip上同时监听某个port
		for _, apiListenAddress := range s.config.APIListenAddresses {
			l, err := net.Listen("tcp", fmt.Sprintf("%s:%v", apiListenAddress, s.config.Port))
			if err != nil {
				s.logger.Warnf("Failed to listen on %v:%v with error: %v", apiListenAddress, s.config.Port, err)
			} else {
				listeners = append(listeners, l)
			}
		}
	}

	if len(listeners) == 0 {
		return errors.Errorf("could not listen on any endpoint")
	}

	for _, listener := range listeners {

		// 服务器是在一个循环中创建的，因为每个实例 都有一个底层监听器的句柄。
		// 通过官方的grpc创建服务实例
		server, err := s.getGRPCServer()
		if err != nil {
			return err
		}
		s.servers = append(s.servers, server)

		// dapr间通信
		if s.kind == internalServer {
			// 注册服务调用服务的 结构体实现
			// 将数据信息封装到了 pkg/proto/internals/v1/service_invocation.pb.go:94
			// 这个结构体里
			// 功能抽象.注册(grpc服务,功能实现)
			internalv1pb.RegisterServiceInvocationServer(server, s.api)
			// app 和 daprd之间通信
		} else if s.kind == apiServer {
			// 功能抽象.注册(grpc服务,功能实现)
			runtimev1pb.RegisterDaprServer(server, s.api)
		}

		go func(server *grpc_go.Server, l net.Listener) {
			if err := server.Serve(l); err != nil {
				s.logger.Fatalf("gRPC serve error: %v", err)
			}
		}(server, listener)
	}
	return nil
}

func (s *server) Close() error {
	for _, server := range s.servers {
		// This calls `Close()` on the underlying listener.
		server.GracefulStop()
	}

	return nil
}

func (s *server) generateWorkloadCert() error {
	//kubectl port-forward svc/dapr-api -n dapr-system 6500:80 &

	s.logger.Info("向Sentry发送工作负荷CSR请求")
	// 发送签名申请，获得证书
	signedCert, err := s.authenticator.CreateSignedWorkloadCert(s.config.AppID, s.config.NameSpace, s.config.TrustDomain)
	if err != nil {
		return errors.Wrap(err, "来自认证器CreateSignedWorkloadCert的错误")
	}
	s.logger.Info("证书签名 successfully")

	tlsCert, err := tls.X509KeyPair(signedCert.WorkloadCert, signedCert.PrivateKeyPem)
	if err != nil {
		return errors.Wrap(err, "error 创建 x509 秘钥对")
	}

	s.signedCert = signedCert
	s.tlsCert = tlsCert
	s.signedCertDuration = signedCert.Expiry.Sub(time.Now().UTC()) // 证书有效期
	return nil
}

// 获取dapr之间 通信时需要的中间件
func (s *server) getMiddlewareOptions() []grpc_go.ServerOption {
	opts := []grpc_go.ServerOption{}
	intr := []grpc_go.UnaryServerInterceptor{}
	intrStream := []grpc_go.StreamServerInterceptor{}

	if len(s.apiSpec.Allowed) > 0 {
		s.logger.Info("enabled API access list on gRPC server")
		intr = append(intr, setAPIEndpointsMiddlewareUnary(s.apiSpec.Allowed))
	}

	if s.authToken != "" {
		s.logger.Info("enabled token authentication on gRPC server")
		intr = append(intr, setAPIAuthenticationMiddlewareUnary(s.authToken, auth.APITokenHeader))
	}
	// todo
	if diag_utils.IsTracingEnabled(s.tracingSpec.SamplingRate) {
		s.logger.Info("enabled gRPC tracing middleware")
		intr = append(intr, diag.GRPCTraceUnaryServerInterceptor(s.config.AppID, s.tracingSpec))

		if s.proxy != nil {
			intrStream = append(intrStream, diag.GRPCTraceStreamServerInterceptor(s.config.AppID, s.tracingSpec))
		}
	}

	if s.metricSpec.Enabled {
		s.logger.Info("enabled gRPC metrics middleware")
		intr = append(intr, diag.DefaultGRPCMonitoring.UnaryServerInterceptor())
	}

	chain := grpc_middleware.ChainUnaryServer(
		intr...,
	)
	opts = append(
		opts,
		grpc_go.UnaryInterceptor(chain),
	)

	if s.proxy != nil {
		chainStream := grpc_middleware.ChainStreamServer(
			intrStream...,
		)

		opts = append(opts, grpc_go.StreamInterceptor(chainStream))
	}

	return opts
}

// 将一些配置，应用到链接上
func (s *server) getGRPCServer() (*grpc_go.Server, error) {
	opts := s.getMiddlewareOptions()
	// 超时时间
	if s.maxConnectionAge != nil {
		opts = append(opts, grpc_go.KeepaliveParams(keepalive.ServerParameters{MaxConnectionAge: *s.maxConnectionAge}))
	}
	// 验证器
	if s.authenticator != nil {
		err := s.generateWorkloadCert()
		if err != nil {
			return nil, err
		}

		// nolint:gosec
		tlsConfig := tls.Config{
			ClientCAs:  s.signedCert.TrustChain, // 证书池
			ClientAuth: tls.RequireAndVerifyClientCert,// 对客户端证书进行校验
			GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				return &s.tlsCert, nil // 返回当前使用的证书、秘钥
			},
		}
		ta := credentials.NewTLS(&tlsConfig)

		opts = append(opts, grpc_go.Creds(ta))
		go s.startWorkloadCertRotation()
	}

	opts = append(opts,
		grpc_go.MaxRecvMsgSize(s.config.MaxRequestBodySize*1024*1024),   // 最大可接收的消息量
		grpc_go.MaxSendMsgSize(s.config.MaxRequestBodySize*1024*1024),   // 最大可发送的消息量
		grpc_go.MaxHeaderListSize(uint32(s.config.ReadBufferSize*1024)), // 最大的header信息大小
	)

	if s.proxy != nil {
		//返回一个ServerOption，允许添加一个自定义的未知服务处理器。所提供的方法是一个双流RPC服务处理程序。
		//处理程序，当收到对未注册的服务或方法的请求时，它将被调用而不是返回 "未实现 "的 gRPC 错误。处理程序和流拦截器（如果设置）可以完全访问ServerStream，包括它的Context。
		opts = append(opts, grpc_go.UnknownServiceHandler(s.proxy.Handler()))
	}

	return grpc_go.NewServer(opts...), nil
}

func (s *server) startWorkloadCertRotation() {
	s.logger.Infof("启动证书过期监视器，当前证书的过期时间: %s", s.signedCert.Expiry.String())
	// 每隔3秒判断一次
	//	allowedClockSkew = time.Minute * 10 时钟偏移
	//	workloadCertTTL = time.Hour * 10  证书的有效期 + allowedClockSkew
	ticker := time.NewTicker(certWatchInterval)

	for range ticker.C {
		s.renewMutex.Lock()
		renew := shouldRenewCert(s.signedCert.Expiry, s.signedCertDuration)
		if renew {
			s.logger.Info("renewing certificate: requesting new cert and restarting gRPC server")

			err := s.generateWorkloadCert()
			if err != nil {
				s.logger.Errorf("error starting server: %s", err)
			}
			diag.DefaultMonitoring.MTLSWorkLoadCertRotationCompleted()
		}
		s.renewMutex.Unlock()
	}
}
// 是不是应该刷新证书
func shouldRenewCert(certExpiryDate time.Time, certDuration time.Duration) bool {
	expiresIn := certExpiryDate.Sub(time.Now().UTC())
	expiresInSeconds := expiresIn.Seconds() // 证书剩余有效秒数
	certDurationSeconds := certDuration.Seconds()
	// 经过了证书有效期的70%就刷新证书
	// 重新签名
	percentagePassed := 100 - ((expiresInSeconds / certDurationSeconds) * 100)
	return percentagePassed >= renewWhenPercentagePassed
}
