package client

import (
	"crypto/x509"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	dapr_credentials "github.com/dapr/dapr/pkg/credentials"
	diag "github.com/dapr/dapr/pkg/diagnostics"
	operatorv1pb "github.com/dapr/dapr/pkg/proto/operator/v1"
)

// GetOperatorClient 返回一个dapr与外部地址通信的grpc 客户端。如果给出了一个证书链，将建立一个TLS连接。
func GetOperatorClient(address, serverName string, certChain *dapr_credentials.CertChain) (operatorv1pb.OperatorClient, *grpc.ClientConn, error) {
	if certChain == nil {
		return nil, nil, errors.New("certificate chain cannot be nil")
	}
	// Unary 统一的
	// 一元装饰器,都生效
	unaryClientInterceptor := grpc_retry.UnaryClientInterceptor()
	//  pkg/diagnostics/grpc_monitoring.go:91
	if diag.DefaultGRPCMonitoring.IsEnabled() {
		//记录 每个RPC的所有请求信息中发送的总字节数
		//记录 按方法和状态计算的RPC数量
		//记录 从发送请求的第一个字节到收到响应的最后一个字节的时间，或终端错误
		//记录 每个RPC的所有响应信息中收到的总字节数
		unaryClientInterceptor = grpc_middleware.ChainUnaryClient(
			unaryClientInterceptor,
			diag.DefaultGRPCMonitoring.UnaryClientInterceptor(),
		)
	}

	opts := []grpc.DialOption{grpc.WithUnaryInterceptor(unaryClientInterceptor)}

	// 证书池
	cp := x509.NewCertPool()
	// 试图解析一系列的PEM编码的证书。它将发现的任何证书附加到s，并报告是否有任何证书被成功解析。
	//在许多Linux系统上，/etc/ssl/cert.pem将包含系统范围内的根CA，其格式适合这个功能。
	ok := cp.AppendCertsFromPEM(certChain.RootCA)
	if !ok {
		return nil, nil, errors.New("未能将PEM根证书追加到x509 CertPool中。")
	}

	config, err := dapr_credentials.TLSConfigFromCertAndKey(certChain.Cert, certChain.Key, serverName, cp)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to create tls config from cert and key")
	}
	// 加密通信
	opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(config)))

	conn, err := grpc.Dial(address, opts...)
	if err != nil {
		return nil, nil, err
	}
	return operatorv1pb.NewOperatorClient(conn), conn, nil
}
