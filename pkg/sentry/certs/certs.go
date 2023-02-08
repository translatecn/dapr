package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"

	"github.com/pkg/errors"
)

const (
	Certificate     = "CERTIFICATE"
	ECPrivateKey    = "EC PRIVATE KEY"
	RSAPrivateKey   = "RSA PRIVATE KEY"
	PKCS8PrivateKey = "PRIVATE KEY"
)

// PrivateKey 包裹一个EC或RSA私钥。
type PrivateKey struct {
	Type string
	Key  interface{}
}

// Credentials 持有一个证书、私钥和信任链。
type Credentials struct {
	PrivateKey  *PrivateKey
	Certificate *x509.Certificate
}

// DecodePEMKey 接收一个密钥PEM字节数组，并返回一个代表PrivateKey的密钥。RSA或EC私钥。
func DecodePEMKey(key []byte) (*PrivateKey, error) {
	block, _ := pem.Decode(key)
	if block == nil {
		return nil, errors.New("key is not PEM encoded")
	}
	switch block.Type {
	case ECPrivateKey:
		k, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return &PrivateKey{Type: ECPrivateKey, Key: k}, nil
	case RSAPrivateKey:
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return &PrivateKey{Type: RSAPrivateKey, Key: k}, nil
	case PKCS8PrivateKey:
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return &PrivateKey{Type: PKCS8PrivateKey, Key: k}, nil
	default:
		return nil, errors.Errorf("unsupported block type %s", block.Type)
	}
}

// DecodePEMCertificates 接收一个PEM编码的x509证书字节数组，并返回 一个x509证书和区块字节数组。
func DecodePEMCertificates(crtb []byte) ([]*x509.Certificate, error) {
	certs := []*x509.Certificate{}
	for len(crtb) > 0 {
		var err error
		var cert *x509.Certificate

		cert, crtb, err = decodeCertificatePEM(crtb)
		if err != nil {
			return nil, err
		}
		if cert != nil {
			// it's a cert, add to pool
			certs = append(certs, cert)
		}
	}
	return certs, nil
}

func decodeCertificatePEM(crtb []byte) (*x509.Certificate, []byte, error) {
	block, crtb := pem.Decode(crtb)
	if block == nil {
		return nil, crtb, errors.New("invalid PEM certificate")
	}
	if block.Type != Certificate {
		return nil, nil, nil
	}
	c, err := x509.ParseCertificate(block.Bytes)
	return c, crtb, err
}

// PEMCredentialsFromFiles 接收一个密钥/证书对的数据，并返回一个带有信任链的有效Credentials包装器。
func PEMCredentialsFromFiles(certPem, keyPem []byte) (*Credentials, error) {
	pk, err := DecodePEMKey(keyPem)
	if err != nil {
		return nil, err
	}

	crts, err := DecodePEMCertificates(certPem)
	if err != nil {
		return nil, err
	}

	if len(crts) == 0 {
		return nil, errors.New("no certificates found")
	}

	match := matchCertificateAndKey(pk, crts[0])
	if !match {
		return nil, errors.New("error validating credentials: public and private key pair do not match")
	}

	creds := &Credentials{
		PrivateKey:  pk,
		Certificate: crts[0],
	}

	return creds, nil
}

func matchCertificateAndKey(pk *PrivateKey, cert *x509.Certificate) bool {
	switch pk.Type {
	case ECPrivateKey:
		key := pk.Key.(*ecdsa.PrivateKey)
		pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
		return ok && pub.X.Cmp(key.X) == 0 && pub.Y.Cmp(key.Y) == 0
	case RSAPrivateKey:
		key := pk.Key.(*rsa.PrivateKey)
		pub, ok := cert.PublicKey.(*rsa.PublicKey)
		return ok && pub.N.Cmp(key.N) == 0 && pub.E == key.E
	default:
		return false
	}
}

func certPoolFromCertificates(certs []*x509.Certificate) *x509.CertPool {
	pool := x509.NewCertPool()
	for _, c := range certs {
		pool.AddCert(c)
	}
	return pool
}

// CertPoolFromPEMString 从PEM编码的证书字符串中返回一个CertPool。
func CertPoolFromPEM(certPem []byte) (*x509.CertPool, error) {
	certs, err := DecodePEMCertificates(certPem)
	if err != nil {
		return nil, err
	}
	if len(certs) == 0 {
		return nil, errors.New("no certificates found")
	}

	return certPoolFromCertificates(certs), nil
}

// ParsePemCSR 构建一个x509证书请求，使用 给定的PEM编码的证书签署请求。
func ParsePemCSR(csrPem []byte) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode(csrPem)
	if block == nil {
		return nil, errors.New("certificate signing request is not properly encoded")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse X.509 certificate signing request")
	}
	return csr, nil
}

// GenerateECPrivateKey 返回一个新的EC私钥。
func GenerateECPrivateKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

//  关于证书这方面的知识
//  https://github.com/bigwhite/experiments/tree/master/gohttps
