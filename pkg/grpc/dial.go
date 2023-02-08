package grpc

import (
	"runtime"

	"github.com/dapr/dapr/pkg/modes"
)

// GetDialAddressPrefix
// 返回gRPC客户端连接的拨号前缀 对于一个给定的DaprMode。
func GetDialAddressPrefix(mode modes.DaprMode) string {
	if runtime.GOOS == "windows" {
		return ""
	}

	switch mode {
	case modes.KubernetesMode:
		return "dns:///"
	default:
		return ""
	}
}
