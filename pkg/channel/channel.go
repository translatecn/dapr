// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package channel

import (
	"context"

	"github.com/dapr/dapr/pkg/config"
	invokev1 "github.com/dapr/dapr/pkg/messaging/v1"
)

const (
	// DefaultChannelAddress 是用户应用程序收听的地址。
	DefaultChannelAddress = "127.0.0.1"
)

// AppChannel 是对与用户代码通信的一种抽象。
type AppChannel interface {
	GetBaseAddress() string // 获取地址
	GetAppConfig() (*config.ApplicationConfig, error) // 获取配置
	InvokeMethod(ctx context.Context, req *invokev1.InvokeMethodRequest) (*invokev1.InvokeMethodResponse, error) // 方法调用
}
