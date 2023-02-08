package security

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"os"
	"sync"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	dapr_credentials "github.com/dapr/dapr/pkg/credentials"
	diag "github.com/dapr/dapr/pkg/diagnostics"
	sentryv1pb "github.com/dapr/dapr/pkg/proto/sentry/v1"
)

const (
	TLSServerName     = "cluster.local"
	sentrySignTimeout = time.Second * 5
	certType          = "CERTIFICATE"
	sentryMaxRetries  = 100
)
// k8s集群内部pod
var kubeTknPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

func GetKubeTknPath() *string {
	return &kubeTknPath
}

type Authenticator interface {
	GetTrustAnchors() *x509.CertPool // 获得信任的锚点
	GetCurrentSignedCert() *SignedCertificate // 获取当前签名了的证书
	CreateSignedWorkloadCert(id, namespace, trustDomain string) (*SignedCertificate, error)// 创建签名负载证书
}

type authenticator struct {
	trustAnchors      *x509.CertPool
	certChainPem      []byte
	keyPem            []byte
	genCSRFunc        func(id string) ([]byte, []byte, error)
	sentryAddress     string
	currentSignedCert *SignedCertificate
	certMutex         *sync.RWMutex
}

type SignedCertificate struct {
	WorkloadCert  []byte // 签名后的证书
	PrivateKeyPem []byte // 私钥
	Expiry        time.Time // 过期的时间点
	TrustChain    *x509.CertPool // 证书链
}

func newAuthenticator(sentryAddress string, trustAnchors *x509.CertPool, certChainPem, keyPem []byte, genCSRFunc func(id string) ([]byte, []byte, error)) Authenticator {
	return &authenticator{
		trustAnchors:  trustAnchors,
		certChainPem:  certChainPem,
		keyPem:        keyPem,
		genCSRFunc:    genCSRFunc,
		sentryAddress: sentryAddress,
		certMutex:     &sync.RWMutex{},
	}
}

// GetTrustAnchors returns the extracted root cert that serves as the trust anchor.
// 返回根证书。
func (a *authenticator) GetTrustAnchors() *x509.CertPool {
	return a.trustAnchors
}

// GetCurrentSignedCert 返回当前的最新的签名的证书
func (a *authenticator) GetCurrentSignedCert() *SignedCertificate {
	a.certMutex.RLock()
	defer a.certMutex.RUnlock()
	return a.currentSignedCert
}

// CreateSignedWorkloadCert  返回一个签名的负载证书，PEM编码的私钥和签名证书的持续时间。
func (a *authenticator) CreateSignedWorkloadCert(id, namespace, trustDomain string) (*SignedCertificate, error) {
	//   pkg/runtime/security/security.go:64
	// 证书签名 , EC 私钥
	csrb, pkPem, err := a.genCSRFunc(id) // id ==APPID 会将它封装到DNSNAMES里  []string
	if err != nil {
		return nil, err
	}
	certPem := pem.EncodeToMemory(&pem.Block{Type: certType, Bytes: csrb})

	config, err := dapr_credentials.TLSConfigFromCertAndKey(a.certChainPem, a.keyPem, TLSServerName, a.trustAnchors)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create tls config from cert and key")
	}

	unaryClientInterceptor := grpc_retry.UnaryClientInterceptor()

	if diag.DefaultGRPCMonitoring.IsEnabled() {
		unaryClientInterceptor = grpc_middleware.ChainUnaryClient(
			unaryClientInterceptor,
			diag.DefaultGRPCMonitoring.UnaryClientInterceptor(),
		)
	}

	conn, err := grpc.Dial(
		a.sentryAddress,
		grpc.WithTransportCredentials(credentials.NewTLS(config)),
		grpc.WithUnaryInterceptor(unaryClientInterceptor))
	if err != nil {
		diag.DefaultMonitoring.MTLSWorkLoadCertRotationFailed("sentry_conn")
		return nil, errors.Wrap(err, "error establishing connection to sentry")
	}
	defer conn.Close()

	c := sentryv1pb.NewCAClient(conn)
	// 签署证书,发送证书前
	//  error validating requester identity: csr validation failed: invalid token: [invalid bearer token, Token has been invalidated]
	//  error validating requester identity: csr validation failed: token/id mismatch. received id: dp-618b5e4aa5ebc3924db86860-workerapp-54683-7f8d646556-vf58h
	resp, err := c.SignCertificate(context.Background(),
		&sentryv1pb.SignCertificateRequest{
			CertificateSigningRequest: certPem,
			Id:                        getSentryIdentifier(id),
			Token:                     getToken(),
			TrustDomain:               trustDomain,
			Namespace:                 namespace,
		}, grpc_retry.WithMax(sentryMaxRetries), grpc_retry.WithPerRetryTimeout(sentrySignTimeout))
	if err != nil {
		diag.DefaultMonitoring.MTLSWorkLoadCertRotationFailed("sign")
		return nil, errors.Wrap(err, "error from sentry SignCertificate")
	}

	workloadCert := resp.GetWorkloadCertificate() // 签名以后的证书
	validTimestamp := resp.GetValidUntil()        // 过期日期
	if err = validTimestamp.CheckValid(); err != nil {
		diag.DefaultMonitoring.MTLSWorkLoadCertRotationFailed("invalid_ts")
		return nil, errors.Wrap(err, "error parsing ValidUntil")
	}
	// 类型转换
	expiry := validTimestamp.AsTime()

	trustChain := x509.NewCertPool()
	for _, c := range resp.GetTrustChainCertificates() {
		ok := trustChain.AppendCertsFromPEM(c)
		if !ok {
			diag.DefaultMonitoring.MTLSWorkLoadCertRotationFailed("chaining")
			return nil, errors.Wrap(err, "failed adding trust chain cert to x509 CertPool")
		}
	}

	signedCert := &SignedCertificate{
		WorkloadCert:  workloadCert,
		PrivateKeyPem: pkPem,
		Expiry:        expiry,
		TrustChain:    trustChain,
	}

	a.certMutex.Lock()
	defer a.certMutex.Unlock()

	a.currentSignedCert = signedCert
	return signedCert, nil
}

// 目前我们支持Kubernetes的身份。
func getToken() string {
	b, _ := os.ReadFile(kubeTknPath)
	return string(b)
}

func getSentryIdentifier(appID string) string {
	// return injected identity, default id if not present
	localID := os.Getenv("SENTRY_LOCAL_IDENTITY")
	if localID != "" {
		return localID
	}
	return appID
}
