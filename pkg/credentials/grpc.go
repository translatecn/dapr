package credentials

import (
	"crypto/tls"
	"crypto/x509"

	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// GetServerOptions 服务端证书验证  中间件
func GetServerOptions(certChain *CertChain) ([]grpc.ServerOption, error) {
	var opts []grpc.ServerOption
	if certChain == nil {
		return opts, nil
	}

	cp := x509.NewCertPool()
	cp.AppendCertsFromPEM(certChain.RootCA)

	cert, err := tls.X509KeyPair(certChain.Cert, certChain.Key)
	if err != nil {
		return opts, nil
	}

	// nolint:gosec
	config := &tls.Config{
		ClientCAs: cp,
		// 需要证书验证
		ClientAuth:   tls.RequireAndVerifyClientCert,
		Certificates: []tls.Certificate{cert},
	}
	opts = append(opts, grpc.Creds(credentials.NewTLS(config)))

	return opts, nil
}

// GetClientOptions 作为客户端，与服务端建立连接
func GetClientOptions(certChain *CertChain, serverName string) ([]grpc.DialOption, error) {
	var opts []grpc.DialOption
	if certChain != nil {
		cp := x509.NewCertPool()
		ok := cp.AppendCertsFromPEM(certChain.RootCA)
		if !ok {
			return nil, errors.New("failed to append PEM root cert to x509 CertPool")
		}

		//  与服务端的配置不同,这是客户端的配置
		//	config := &tls.Config{
		//		InsecureSkipVerify: false,
		//		RootCAs:            rootCA,
		//		ServerName:         serverName,
		//		Certificates:       []tls.Certificate{cert},
		//	}

		config, err := TLSConfigFromCertAndKey(certChain.Cert, certChain.Key, serverName, cp)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create tls config from cert and key")
		}
		//配置数据传输加密
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(config)))
	} else {
		//禁用传输安全性
		opts = append(opts, grpc.WithInsecure())
	}
	return opts, nil
}

// TLSConfigFromCertAndKey 返回一个tls.config对象从已经验证过的PEM格式的证书、秘钥对
func TLSConfigFromCertAndKey(certPem, keyPem []byte, serverName string, rootCA *x509.CertPool) (*tls.Config, error) {
	//从一对PEM编码的数据中解析出一个公钥/私钥对。
	//在成功返回时，Certificate.Leaf将为nil，因为证书的解析形式没有被保留。
	cert, err := tls.X509KeyPair(certPem, keyPem)
	if err != nil {
		return nil, err
	}

	// nolint:gosec
	config := &tls.Config{
		InsecureSkipVerify: false,
		RootCAs:            rootCA,
		//用来验证返回的证书上的主机名，除非给出InsecureSkipVerify。它也包括在客户端的握手中，以支持虚拟主机，除非它是一个IP地址。
		ServerName:   serverName,
		Certificates: []tls.Certificate{cert},
	}

	return config, nil
}
