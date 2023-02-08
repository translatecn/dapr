// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package http

// ServerConfig  HTTP server 的配置
type ServerConfig struct {
	AllowedOrigins     string
	AppID              string
	HostAddress        string
	Port               int
	APIListenAddresses []string
	PublicPort         *int // 服务暴露的端口
	ProfilePort        int  // 性能分析暴露的端口
	EnableProfiling    bool // 是否允许 性能分析
	MaxRequestBodySize int
	UnixDomainSocket   string
	ReadBufferSize     int
	StreamRequestBody  bool
}

// NewServerConfig 返回HTTP server 配置
func NewServerConfig(appID string, hostAddress string, port int, apiListenAddresses []string, publicPort *int, profilePort int, allowedOrigins string, enableProfiling bool, maxRequestBodySize int, unixDomainSocket string, readBufferSize int, streamRequestBody bool) ServerConfig {
	return ServerConfig{
		AllowedOrigins:     allowedOrigins,
		AppID:              appID,
		HostAddress:        hostAddress,
		Port:               port,
		APIListenAddresses: apiListenAddresses,
		PublicPort:         publicPort,
		ProfilePort:        profilePort,
		EnableProfiling:    enableProfiling,
		MaxRequestBodySize: maxRequestBodySize,
		UnixDomainSocket:   unixDomainSocket,
		ReadBufferSize:     readBufferSize,
		StreamRequestBody:  streamRequestBody,
	}
}
