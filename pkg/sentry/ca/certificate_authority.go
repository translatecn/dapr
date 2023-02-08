package ca

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/dapr/kit/logger"

	"github.com/dapr/dapr/pkg/credentials"
	"github.com/dapr/dapr/pkg/sentry/certs"
	"github.com/dapr/dapr/pkg/sentry/config"
	"github.com/dapr/dapr/pkg/sentry/csr"
	"github.com/dapr/dapr/pkg/sentry/identity"
)

const (
	caOrg                      = "dapr.io/sentry"
	caCommonName               = "cluster.local"
	selfSignedRootCertLifetime = time.Hour * 8760
	certLoadTimeout            = time.Second * 30
	certDetectInterval         = time.Second * 1
)

var log = logger.NewLogger("dapr.sentry.ca")

// CertificateAuthority 代表一个符合要求的证书颁发机构的接口。
// 其职责包括加载信任锚和发行人的证书，提供对信任包的安全访问。 验证和签署CSR。
type CertificateAuthority interface {
	LoadOrStoreTrustBundle() error
	GetCACertBundle() TrustRootBundler
	SignCSR(csrPem []byte, subject string, identity *identity.Bundle, ttl time.Duration, isCA bool) (*SignedCertificate, error)
	ValidateCSR(csr *x509.CertificateRequest) error
}

func NewCertificateAuthority(config config.SentryConfig) (CertificateAuthority, error) {
	// 从contrib组件加载外部CAs
	switch config.CAStore {
	default:
		return &defaultCA{
			config:     config,
			issuerLock: &sync.RWMutex{},
		}, nil
	}
}

type defaultCA struct {
	bundle     *trustRootBundle
	config     config.SentryConfig
	issuerLock *sync.RWMutex
}

type SignedCertificate struct {
	Certificate *x509.Certificate
	CertPEM     []byte
}

// LoadOrStoreTrustBundle
// 从配置的秘密存储区加载根证书和颁发者证书。
// 执行验证，并创建一个受保护的信任包，其中包含信任锚和颁发者凭据。
// 如果成功，将启动一个观察程序来跟踪发行者的到期日。
func (c *defaultCA) LoadOrStoreTrustBundle() error {
	bundle, err := c.validateAndBuildTrustBundle()
	if err != nil {
		return err
	}

	c.bundle = bundle
	return nil
}

// GetCACertBundle 返回根证书绑定。
func (c *defaultCA) GetCACertBundle() TrustRootBundler {
	return c.bundle
}

// SignCSR 用一个PEM编码的CSR证书和持续时间来签署请求。
//如果 isCA 被设置为 true，将签发一个 CA 证书。如果isCA被设置为false，则将签发一个工作量证书。
func (c *defaultCA) SignCSR(csrPem []byte, subject string, identity *identity.Bundle, ttl time.Duration, isCA bool) (*SignedCertificate, error) {
	c.issuerLock.RLock()
	defer c.issuerLock.RUnlock()

	certLifetime := ttl
	if certLifetime.Seconds() < 0 {
		certLifetime = c.config.WorkloadCertTTL
	}

	certLifetime += c.config.AllowedClockSkew

	signingCert := c.bundle.issuerCreds.Certificate
	signingKey := c.bundle.issuerCreds.PrivateKey

	cert, err := certs.ParsePemCSR(csrPem)
	if err != nil {
		return nil, errors.Wrap(err, "error parsing csr pem")
	}

	crtb, err := csr.GenerateCSRCertificate(cert, subject, identity, signingCert, cert.PublicKey, signingKey.Key, certLifetime, c.config.AllowedClockSkew, isCA)
	if err != nil {
		return nil, errors.Wrap(err, "error signing csr")
	}

	csrCert, err := x509.ParseCertificate(crtb)
	if err != nil {
		return nil, errors.Wrap(err, "error parsing cert")
	}

	certPem := pem.EncodeToMemory(&pem.Block{
		Type:  certs.Certificate,
		Bytes: crtb,
	})

	return &SignedCertificate{
		Certificate: csrCert,
		CertPEM:     certPem,
	}, nil
}

func (c *defaultCA) ValidateCSR(csr *x509.CertificateRequest) error {
	if csr.Subject.CommonName == "" {
		return errors.New("cannot validate request: missing common name")
	}
	return nil
}
// 根据文件存不存在，判断需不需要创建
func shouldCreateCerts(conf config.SentryConfig) bool {
	exists, err := certs.CredentialsExist(conf)
	if err != nil {
		log.Errorf("error checking if credentials exist: %s", err)
	}
	if exists {
		return false
	}

	if _, err = os.Stat(conf.RootCertPath); os.IsNotExist(err) {
		return true
	}
	fInfo, err := os.Stat(conf.IssuerCertPath)
	if os.IsNotExist(err) || fInfo.Size() == 0 {
		return true
	}
	return false
}

//判断根证书是否加载完毕
func detectCertificates(path string) error {
	t := time.NewTicker(certDetectInterval) // 一秒
	timeout := time.After(certLoadTimeout)  // 等待证书加载超时时间

	for {
		select {
		case <-timeout:
			return errors.New("timed out on detecting credentials on filesystem")
		case <-t.C:
			_, err := os.Stat(path)
			if err == nil {
				t.Stop()
				return nil
			}
		}
	}
}

func (c *defaultCA) validateAndBuildTrustBundle() (*trustRootBundle, error) {
	var (
		issuerCreds     *certs.Credentials
		rootCertBytes   []byte
		issuerCertBytes []byte
	)

	// certs exist on disk or getting created, load them when ready
	// 证书存在于磁盘上或正在创建中，准备好时加载它们
	if !shouldCreateCerts(c.config) {
		err := detectCertificates(c.config.RootCertPath)
		if err != nil {
			return nil, err
		}

		certChain, err := credentials.LoadFromDisk(c.config.RootCertPath, c.config.IssuerCertPath, c.config.IssuerKeyPath)
		if err != nil {
			return nil, errors.Wrap(err, "error loading cert chain from disk")
		}

		issuerCreds, err = certs.PEMCredentialsFromFiles(certChain.Cert, certChain.Key)
		if err != nil {
			return nil, errors.Wrap(err, "error reading PEM credentials")
		}

		rootCertBytes = certChain.RootCA
		issuerCertBytes = certChain.Cert
	} else {
		// create self signed root and issuer certs
		log.Info("root and issuer certs not found: generating self signed CA")
		var err error
		issuerCreds, rootCertBytes, issuerCertBytes, err = c.generateRootAndIssuerCerts()
		if err != nil {
			return nil, errors.Wrap(err, "error generating trust root bundle")
		}

		log.Info("self signed certs generated and persisted successfully")
	}

	// 加载信任锚
	trustAnchors, err := certs.CertPoolFromPEM(rootCertBytes)
	if err != nil {
		return nil, errors.Wrap(err, "error parsing cert pool for trust anchors")
	}

	return &trustRootBundle{
		issuerCreds:   issuerCreds,
		trustAnchors:  trustAnchors,
		trustDomain:   c.config.TrustDomain,
		rootCertPem:   rootCertBytes,
		issuerCertPem: issuerCertBytes,
	}, nil
}

//生成根、颁发者证书  return 颁发者证书结构体、根证书字节、颁发者证书字节
func (c *defaultCA) generateRootAndIssuerCerts() (*certs.Credentials, []byte, []byte, error) {
	//都是使用的ecdsa签名算法
	rootKey, err := certs.GenerateECPrivateKey() // 根秘钥
	if err != nil {
		return nil, nil, nil, err
	}
	rootCsr, err := csr.GenerateRootCertCSR(caOrg, caCommonName, &rootKey.PublicKey, selfSignedRootCertLifetime, c.config.AllowedClockSkew) // 证书签名请求
	if err != nil {
		return nil, nil, nil, err
	}

	rootCertBytes, err := x509.CreateCertificate(rand.Reader, rootCsr, rootCsr, &rootKey.PublicKey, rootKey) // 根证书 二进制数据
	if err != nil {
		return nil, nil, nil, err
	}

	rootCertPem := pem.EncodeToMemory(&pem.Block{Type: certs.Certificate, Bytes: rootCertBytes}) // 根证书

	rootCert, err := x509.ParseCertificate(rootCertBytes) // 根证书 struct
	if err != nil {
		return nil, nil, nil, err
	}

	issuerKey, err := certs.GenerateECPrivateKey() // 生成颁发者证书
	if err != nil {
		return nil, nil, nil, err
	}
	// 颁发者签名
	issuerCsr, err := csr.GenerateIssuerCertCSR(caCommonName, &issuerKey.PublicKey, selfSignedRootCertLifetime, c.config.AllowedClockSkew)
	if err != nil {
		return nil, nil, nil, err
	}

	issuerCertBytes, err := x509.CreateCertificate(rand.Reader, issuerCsr, rootCert, &issuerKey.PublicKey, rootKey)
	if err != nil {
		return nil, nil, nil, err
	}

	issuerCertPem := pem.EncodeToMemory(&pem.Block{Type: certs.Certificate, Bytes: issuerCertBytes})

	encodedKey, err := x509.MarshalECPrivateKey(issuerKey)
	if err != nil {
		return nil, nil, nil, err
	}
	issuerKeyPem := pem.EncodeToMemory(&pem.Block{Type: certs.ECPrivateKey, Bytes: encodedKey})

	issuerCert, err := x509.ParseCertificate(issuerCertBytes)
	if err != nil {
		return nil, nil, nil, err
	}

	// 储存证书，以便下次sentry重新启动时能正常加载。
	err = certs.StoreCredentials(c.config, rootCertPem, issuerCertPem, issuerKeyPem)
	if err != nil {
		return nil, nil, nil, err
	}

	return &certs.Credentials{
		PrivateKey: &certs.PrivateKey{
			Type: certs.ECPrivateKey,
			Key:  issuerKey,
		},
		Certificate: issuerCert,
	}, rootCertPem, issuerCertPem, nil
}
