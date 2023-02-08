package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/dapr/kit/logger"

	sentryv1pb "github.com/dapr/dapr/pkg/proto/sentry/v1"
	"github.com/dapr/dapr/pkg/sentry/ca"
	"github.com/dapr/dapr/pkg/sentry/certs"
	"github.com/dapr/dapr/pkg/sentry/csr"
	"github.com/dapr/dapr/pkg/sentry/identity"
	"github.com/dapr/dapr/pkg/sentry/monitoring"
)

const (
	serverCertExpiryBuffer = time.Minute * 15
)

var log = logger.NewLogger("dapr.sentry.server")

// CAServer 是证书颁发机构服务器的一个接口。
type CAServer interface {
	Run(port int, trustBundle ca.TrustRootBundler) error
	Shutdown()
}

type server struct {
	certificate *tls.Certificate
	certAuth    ca.CertificateAuthority
	srv         *grpc.Server
	validator   identity.Validator
}

// NewCAServer 返回一个运行gRPC服务器的新CA服务器。
func NewCAServer(ca ca.CertificateAuthority, validator identity.Validator) CAServer {
	return &server{
		certAuth:  ca,
		validator: validator,
	}
}

// Run 为Sentry证书颁发机构启动一个安全的gRPC服务器。
// 它使用信任的根证书强制执行客户端的证书验证。
func (s *server) Run(port int, trustBundler ca.TrustRootBundler) error {
	addr := fmt.Sprintf(":%v", port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrapf(err, "could not listen on %s", addr)
	}

	tlsOpt := s.tlsServerOption(trustBundler)
	s.srv = grpc.NewServer(tlsOpt)
	sentryv1pb.RegisterCAServer(s.srv, s)

	if err := s.srv.Serve(lis); err != nil {
		return errors.Wrap(err, "grpc serve error")
	}
	return nil
}

func (s *server) tlsServerOption(trustBundler ca.TrustRootBundler) grpc.ServerOption {
	cp := trustBundler.GetTrustAnchors()

	// nolint:gosec
	config := &tls.Config{
		ClientCAs: cp,
		// Require cert verification
		ClientAuth: tls.RequireAndVerifyClientCert,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			if s.certificate == nil || needsRefresh(s.certificate, serverCertExpiryBuffer) {
				cert, err := s.getServerCertificate()
				if err != nil {
					monitoring.ServerCertIssueFailed("server_cert")
					log.Error(err)
					return nil, errors.Wrap(err, "failed to get TLS server certificate")
				}
				s.certificate = cert
			}
			return s.certificate, nil
		},
	}
	return grpc.Creds(credentials.NewTLS(config))
}

func (s *server) getServerCertificate() (*tls.Certificate, error) {
	// 生成新的证书签名文件、私钥文件
	csrPem, pkPem, err := csr.GenerateCSR("", false)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	// CA证书的过期时间
	issuerExp := s.certAuth.GetCACertBundle().GetIssuerCertExpiry()
	serverCertTTL := issuerExp.Sub(now)
	//	证书申请: csrCert,	 证书:     certPem,
	resp, err := s.certAuth.SignCSR(csrPem, s.certAuth.GetCACertBundle().GetTrustDomain(), nil, serverCertTTL, false)
	if err != nil {
		return nil, err
	}

	certPem := resp.CertPEM // 新生成的证书
	// 以下的证书都是CA， 都设置了ISCA:true
	certPem = append(certPem, s.certAuth.GetCACertBundle().GetIssuerCertPem()...) // 使用此证书进行的签名
	certPem = append(certPem, s.certAuth.GetCACertBundle().GetRootCertPem()...)

	cert, err := tls.X509KeyPair(certPem, pkPem)
	if err != nil {
		return nil, err
	}

	return &cert, nil
}

// SignCertificate 处理来自Dapr sidecar 的CSR请求。
// 该方法接收一个带有身份和初始证书的请求 并返回一个包括信任链的签名证书给调用者，并附上一个到期日。
func (s *server) SignCertificate(ctx context.Context, req *sentryv1pb.SignCertificateRequest) (*sentryv1pb.SignCertificateResponse, error) {
	monitoring.CertSignRequestReceived()
	// 获取签名申请的字节数据
	csrPem := req.GetCertificateSigningRequest()
	// CertificateRequest
	csr, err := certs.ParsePemCSR(csrPem)
	if err != nil {
		err = errors.Wrap(err, "cannot parse certificate signing request pem")
		log.Error(err)
		monitoring.CertSignFailed("cert_parse")
		return nil, err
	}
	// cn字段不能为空
	err = s.certAuth.ValidateCSR(csr)
	if err != nil {
		err = errors.Wrap(err, "error validating csr")
		log.Error(err)
		monitoring.CertSignFailed("cert_validation")
		return nil, err
	}

	// 不能为空,校验token是不是这个id的
	err = s.validator.Validate(req.GetId(), req.GetToken(), req.GetNamespace())
	if err != nil {
		err = errors.Wrap(err, "error validating requester identity")
		log.Error(err)
		monitoring.CertSignFailed("req_id_validation")
		return nil, err
	}
	//
	identity := identity.NewBundle(csr.Subject.CommonName, req.GetNamespace(), req.GetTrustDomain())
	signed, err := s.certAuth.SignCSR(csrPem, csr.Subject.CommonName, identity, -1, false)
	if err != nil {
		err = errors.Wrap(err, "error signing csr")
		log.Error(err)
		monitoring.CertSignFailed("cert_sign")
		return nil, err
	}

	certPem := signed.CertPEM
	issuerCert := s.certAuth.GetCACertBundle().GetIssuerCertPem()
	rootCert := s.certAuth.GetCACertBundle().GetRootCertPem()

	certPem = append(certPem, issuerCert...)
	certPem = append(certPem, rootCert...)

	if len(certPem) == 0 {
		err = errors.New("insufficient data in certificate signing request, no certs signed")
		log.Error(err)
		monitoring.CertSignFailed("insufficient_data")
		return nil, err
	}

	expiry := timestamppb.New(signed.Certificate.NotAfter)
	if err = expiry.CheckValid(); err != nil {
		return nil, errors.Wrap(err, "could not validate certificate validity")
	}

	resp := &sentryv1pb.SignCertificateResponse{
		WorkloadCertificate:    certPem,
		TrustChainCertificates: [][]byte{issuerCert, rootCert},
		ValidUntil:             expiry,
	}

	monitoring.CertSignSucceed()

	return resp, nil
}

func (s *server) Shutdown() {
	s.srv.Stop()
}

// 有没有过期
func needsRefresh(cert *tls.Certificate, expiryBuffer time.Duration) bool {
	leaf := cert.Leaf
	if leaf == nil {
		return true
	}

	//检查叶子证书是否即将过期。
	return leaf.NotAfter.Add(-serverCertExpiryBuffer).Before(time.Now().UTC())
}
