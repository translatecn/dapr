// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package runtime

import (
	config "github.com/dapr/dapr/pkg/config/modes" // ok
	"github.com/dapr/dapr/pkg/credentials"         // ok
	"github.com/dapr/dapr/pkg/modes"               // ok
)

// Protocol 是一种通信协议。
type Protocol string

//pkg/config/env/env_variables.go:4

const (
	// GRPCProtocol 是一个gRPC通信协议。
	GRPCProtocol Protocol = "grpc"
	// HTTPProtocol 是一个http通信协议。
	HTTPProtocol Protocol = "http"
	// DefaultDaprHTTPPort 是Dapr的默认http端口。
	DefaultDaprHTTPPort = 3500
	// DefaultDaprPublicPort 是Dapr的默认http端口。
	DefaultDaprPublicPort = 3501
	// DefaultDaprAPIGRPCPort 是Dapr的默认API gRPC端口。
	DefaultDaprAPIGRPCPort = 50001
	// DefaultProfilePort 性能分析的默认端口。
	DefaultProfilePort = 7777
	// DefaultMetricsPort 指标监控的默认端口
	DefaultMetricsPort = 9090
	// DefaultMaxRequestBodySize 是Dapr HTTP服务器的最大请求体大小的默认选项，单位是MB。
	DefaultMaxRequestBodySize = 4
	// DefaultAPIListenAddress 是监听Dapr HTTP和GRPC APIs的地址。空字符串是所有地址。
	DefaultAPIListenAddress = ""
	// DefaultReadBufferSize  是Dapr HTTP服务器最大header大小的默认选项，单位为KB。
	DefaultReadBufferSize = 4
)

// Config 持有Dapr Runtime配置。
type Config struct {
	ID                   string // 应用ID
	HTTPPort             int
	PublicPort           *int
	ProfilePort          int
	EnableProfiling      bool
	APIGRPCPort          int
	InternalGRPCPort     int
	ApplicationPort      int
	APIListenAddresses   []string
	ApplicationProtocol  Protocol
	Mode                 modes.DaprMode
	PlacementAddresses   []string
	GlobalConfig         string
	AllowedOrigins       string
	Standalone           config.StandaloneConfig
	Kubernetes           config.KubernetesConfig
	MaxConcurrency       int
	mtlsEnabled          bool
	SentryServiceAddress string
	CertChain            *credentials.CertChain
	AppSSL               bool
	MaxRequestBodySize   int
	UnixDomainSocket     string
	ReadBufferSize       int
	StreamRequestBody    bool
}

// NewRuntimeConfig 返回运行时配置
func NewRuntimeConfig(
	id string, placementAddresses []string,
	controlPlaneAddress, allowedOrigins, globalConfig, componentsPath, appProtocol, mode string,
	httpPort, internalGRPCPort, apiGRPCPort int, apiListenAddresses []string, publicPort *int, appPort, profilePort int,
	enableProfiling bool, maxConcurrency int, mtlsEnabled bool, sentryAddress string, appSSL bool, maxRequestBodySize int, unixDomainSocket string, readBufferSize int, streamRequestBody bool) *Config {
	return &Config{
		ID:                  id,
		HTTPPort:            httpPort,
		PublicPort:          publicPort,
		InternalGRPCPort:    internalGRPCPort,
		APIGRPCPort:         apiGRPCPort,
		ApplicationPort:     appPort,
		ProfilePort:         profilePort,
		APIListenAddresses:  apiListenAddresses,
		ApplicationProtocol: Protocol(appProtocol),
		Mode:                modes.DaprMode(mode),
		PlacementAddresses:  placementAddresses,
		GlobalConfig:        globalConfig,
		AllowedOrigins:      allowedOrigins,
		Standalone: config.StandaloneConfig{
			ComponentsPath: componentsPath,
		},
		Kubernetes: config.KubernetesConfig{
			ControlPlaneAddress: controlPlaneAddress,
		},
		EnableProfiling:      enableProfiling,
		MaxConcurrency:       maxConcurrency,
		mtlsEnabled:          mtlsEnabled,
		SentryServiceAddress: sentryAddress,
		AppSSL:               appSSL,
		MaxRequestBodySize:   maxRequestBodySize,
		UnixDomainSocket:     unixDomainSocket,
		ReadBufferSize:       readBufferSize,
		StreamRequestBody:    streamRequestBody,
	}
}
