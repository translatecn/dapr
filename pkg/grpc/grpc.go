// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package grpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/dapr/dapr/pkg/channel"
	grpc_channel "github.com/dapr/dapr/pkg/channel/grpc"
	"github.com/dapr/dapr/pkg/config"
	diag "github.com/dapr/dapr/pkg/diagnostics"
	"github.com/dapr/dapr/pkg/modes"
	"github.com/dapr/dapr/pkg/runtime/security"
)

const (
	//需要对具有多个端点（即多个实例）的目标服务请求进行负载平衡。  轮询
	grpcServiceConfig = `{"loadBalancingPolicy":"round_robin"}`
	dialTimeout       = time.Second * 30
)

// ClientConnCloser 接口封装
type ClientConnCloser interface {
	grpc.ClientConnInterface
	io.Closer
}

// Manager
// 是一个gRPC连接池的包装器。
type Manager struct {
	AppClient      ClientConnCloser
	lock           *sync.RWMutex
	connectionPool map[string]*grpc.ClientConn
	auth           security.Authenticator
	mode           modes.DaprMode
}

// NewGRPCManager 返回grpc管理器
func NewGRPCManager(mode modes.DaprMode) *Manager {
	return &Manager{
		lock:           &sync.RWMutex{},
		connectionPool: map[string]*grpc.ClientConn{},
		mode:           mode,
	}
}

// SetAuthenticator 设置grpc认证器
func (g *Manager) SetAuthenticator(auth security.Authenticator) {
	g.auth = auth
}

// CreateLocalChannel 创建 gRPC AppChannel.
func (g *Manager) CreateLocalChannel(port, maxConcurrency int, spec config.TracingSpec, sslEnabled bool, maxRequestBodySize int, readBufferSize int) (channel.AppChannel, error) {
	//  eg  dns:///127.0.0.1:3001 链接的app
	conn, err := g.GetGRPCConnection(context.TODO(), fmt.Sprintf("127.0.0.1:%v", port), "", "", true, false, sslEnabled)
	if err != nil {
		return nil, errors.Errorf("error 建立连接to app grpc on port %v: %s", port, err)
	}

	g.AppClient = conn
	ch := grpc_channel.CreateLocalChannel(port, maxConcurrency, conn, spec, maxRequestBodySize, readBufferSize)
	return ch, nil
}

// GetGRPCConnection
// 为给定的地址返回一个新的grpc连接，如果不存在则输入一个。
func (g *Manager) GetGRPCConnection(ctx context.Context, address, id string,
	namespace string,
	skipTLS, recreateIfExists, sslEnabled bool,
	customOpts ...grpc.DialOption) (*grpc.ClientConn, error) {
	g.lock.RLock()
	// 判断是否已经已经存在，且不用重新创建  dp-61c2cb20562850d49d47d1c7-executorapp-dapr.mesoid.svc.cluster.local:50004
	// 应用+grpc内部通信地址
	if val, ok := g.connectionPool[address]; ok && !recreateIfExists {
		g.lock.RUnlock()
		return val, nil
	}
	g.lock.RUnlock()

	g.lock.Lock()
	defer g.lock.Unlock()
	// 再次读取该值，可能会已被并发创建
	if val, ok := g.connectionPool[address]; ok && !recreateIfExists {
		return val, nil
	}

	opts := []grpc.DialOption{
		// grpc 服务配置
		grpc.WithDefaultServiceConfig(grpcServiceConfig),
	}
	// 是否启用监控, 一般来说都是开启的
	if diag.DefaultGRPCMonitoring.IsEnabled() {
		// 添加拦截器
		opts = append(opts, grpc.WithUnaryInterceptor(diag.DefaultGRPCMonitoring.UnaryClientInterceptor()))
	}
	// dapr 与 app grpc通信需不需要加证书
	transportCredentialsAdded := false
	// 不跳过，且设置了证书
	if !skipTLS && g.auth != nil {
		signedCert := g.auth.GetCurrentSignedCert() // 获取签名的证书链
		//从一对PEM编码的数据中解析出一个公钥/私钥对。在成功返回时，Certificate.Leaf将为nil，因为证书的解析形式没有被保留。
		cert, err := tls.X509KeyPair(signedCert.WorkloadCert, signedCert.PrivateKeyPem)
		if err != nil {
			return nil, errors.Errorf("error 生成 x509 秘钥对: %s", err)
		}

		var serverName string
		if id != "cluster.local" {
			serverName = fmt.Sprintf("%s.%s.svc.cluster.local", id, namespace)
		}

		// nolint:gosec
		ta := credentials.NewTLS(&tls.Config{
			ServerName:   serverName,
			Certificates: []tls.Certificate{cert},
			RootCAs:      signedCert.TrustChain,
		})
		opts = append(opts, grpc.WithTransportCredentials(ta))
		transportCredentialsAdded = true
	}

	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	// 		dns:///
	dialPrefix := GetDialAddressPrefix(g.mode)
	if sslEnabled {
		// nolint:gosec
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true,
		})))
		transportCredentialsAdded = true
	}

	if !transportCredentialsAdded {
		// 不安全传输
		opts = append(opts, grpc.WithInsecure())
	}
	// 一些自定义的配置
	opts = append(opts, customOpts...)
	conn, err := grpc.DialContext(ctx, dialPrefix+address, opts...)
	if err != nil {
		return nil, err
	}
	// 将旧的停掉
	if c, ok := g.connectionPool[address]; ok {
		c.Close()
	}

	g.connectionPool[address] = conn

	return conn, nil
}
