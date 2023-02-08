package csr

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	"github.com/pkg/errors"

	"github.com/dapr/dapr/pkg/sentry/certs"
	"github.com/dapr/dapr/pkg/sentry/identity"
)

const (
	blockTypeECPrivateKey = "EC PRIVATE KEY" // EC 类型秘钥
	blockTypePrivateKey   = "PRIVATE KEY"    // PKCS# 8个普通私钥
	encodeMsgCSR          = "CERTIFICATE REQUEST"
	encodeMsgCert         = "CERTIFICATE"
)

// SAN扩展的OID (http://www.alvestrand.no/objectid/2.5.29.17.html)
var oidSubjectAlternativeName = asn1.ObjectIdentifier{2, 5, 29, 17}

// GenerateCSR 创建一个X.509证书签名请求和私钥。"", false
func GenerateCSR(org string, pkcs8 bool) ([]byte, []byte, error) {
	key, err := certs.GenerateECPrivateKey()
	if err != nil {
		return nil, nil, errors.Wrap(err, "unable to generate private keys")
	}

	templ := &x509.CertificateRequest{
		Subject: pkix.Name{
			Organization: []string{org},
		},
	}

	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, templ, crypto.PrivateKey(key))
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to create CSR")
	}

	crtPem, keyPem, err := encode(true, csrBytes, key, pkcs8)
	return crtPem, keyPem, err
}

func genCSRTemplate(org string) (*x509.CertificateRequest, error) {
	return &x509.CertificateRequest{
		Subject: pkix.Name{
			Organization: []string{org},
		},
	}, nil
}

// generateBaseCert  返回一个基本的非CA证书，该证书可以成为一个工作负载或CA证书。通过添加主体、密钥使用和附加属性。
func generateBaseCert(ttl, skew time.Duration, publicKey interface{}) (*x509.Certificate, error) {
	serNum, err := newSerialNumber()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	// 证书的有效时间
	notBefore := now.Add(-1 * skew)
	notAfter := now.Add(ttl)

	return &x509.Certificate{
		SerialNumber: serNum,
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		PublicKey:    publicKey,
	}, nil
}

// GenerateIssuerCertCSR 生成颁发者证书、ca证书
func GenerateIssuerCertCSR(cn string, publicKey interface{}, ttl, skew time.Duration) (*x509.Certificate, error) {
	cert, err := generateBaseCert(ttl, skew, publicKey)
	if err != nil {
		return nil, err
	}

	cert.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	cert.Subject = pkix.Name{
		CommonName: cn,
	}
	cert.DNSNames = []string{cn}
	cert.IsCA = true
	cert.BasicConstraintsValid = true
	cert.SignatureAlgorithm = x509.ECDSAWithSHA256
	return cert, nil
}

// GenerateRootCertCSR 返回一个CA根证书 x509证书。
func GenerateRootCertCSR(org, cn string, publicKey interface{}, ttl, skew time.Duration) (*x509.Certificate, error) {
	cert, err := generateBaseCert(ttl, skew, publicKey)
	if err != nil {
		return nil, err
	}

	cert.KeyUsage = x509.KeyUsageCertSign
	cert.ExtKeyUsage = append(cert.ExtKeyUsage, x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)
	cert.Subject = pkix.Name{
		CommonName:   cn,
		Organization: []string{org},
	}
	cert.DNSNames = []string{cn}
	cert.IsCA = true
	cert.BasicConstraintsValid = true
	cert.SignatureAlgorithm = x509.ECDSAWithSHA256
	return cert, nil
}

// GenerateCSRCertificate 返回来自CSR、签名证书、公钥、签名私钥和有期限的x509证书。
func GenerateCSRCertificate(csr *x509.CertificateRequest, subject string, identityBundle *identity.Bundle, signingCert *x509.Certificate, publicKey interface{}, signingKey crypto.PrivateKey,
	ttl, skew time.Duration, isCA bool) ([]byte, error) {
	cert, err := generateBaseCert(ttl, skew, publicKey)
	if err != nil {
		return nil, errors.Wrap(err, "error generating csr certificate")
	}
	if isCA {
		cert.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	} else {
		cert.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
		cert.ExtKeyUsage = append(cert.ExtKeyUsage, x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)
	}

	if subject == "cluster.local" {
		cert.Subject = pkix.Name{
			CommonName: subject,
		}
		cert.DNSNames = []string{subject}
	}

	cert.Issuer = signingCert.Issuer
	cert.IsCA = isCA
	cert.IPAddresses = csr.IPAddresses
	cert.Extensions = csr.Extensions
	cert.BasicConstraintsValid = true
	cert.SignatureAlgorithm = csr.SignatureAlgorithm

	if identityBundle != nil {
		spiffeID, err := identity.CreateSPIFFEID(identityBundle.TrustDomain, identityBundle.Namespace, identityBundle.ID)
		if err != nil {
			return nil, errors.Wrap(err, "error generating spiffe id")
		}

		rv := []asn1.RawValue{
			{
				Bytes: []byte(spiffeID),
				Class: asn1.ClassContextSpecific,
				Tag:   asn1.TagOID,
			},
			{
				Bytes: []byte(fmt.Sprintf("%s.%s.svc.cluster.local", subject, identityBundle.Namespace)),
				Class: asn1.ClassContextSpecific,
				Tag:   2,
			},
		}

		b, err := asn1.Marshal(rv)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal asn1 raw value for spiffe id")
		}

		cert.ExtraExtensions = append(cert.ExtraExtensions, pkix.Extension{
			Id:       oidSubjectAlternativeName,
			Value:    b,
			Critical: true, // According to x509 and SPIFFE specs, a SubjAltName extension must be critical if subject name and DNS are not present.
		})
	}

	return x509.CreateCertificate(rand.Reader, cert, signingCert, publicKey, signingKey)
}

//	crtPem, keyPem, err := encode(true, csrBytes, key, false)
func encode(csr bool, csrOrCert []byte, privKey *ecdsa.PrivateKey, pkcs8 bool) ([]byte, []byte, error) {
	encodeMsg := encodeMsgCert
	if csr {
		encodeMsg = encodeMsgCSR
	}
	csrOrCertPem := pem.EncodeToMemory(&pem.Block{Type: encodeMsg, Bytes: csrOrCert})

	var encodedKey, privPem []byte
	var err error

	if pkcs8 {
		if encodedKey, err = x509.MarshalPKCS8PrivateKey(privKey); err != nil {
			return nil, nil, err
		}
		privPem = pem.EncodeToMemory(&pem.Block{Type: blockTypePrivateKey, Bytes: encodedKey})
	} else {
		encodedKey, err = x509.MarshalECPrivateKey(privKey)
		if err != nil {
			return nil, nil, err
		}
		privPem = pem.EncodeToMemory(&pem.Block{Type: blockTypeECPrivateKey, Bytes: encodedKey})
	}
	return csrOrCertPem, privPem, nil
}

// 生成证书编号
func newSerialNumber() (*big.Int, error) {
	serialNumLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNum, err := rand.Int(rand.Reader, serialNumLimit)
	if err != nil {
		return nil, errors.Wrap(err, "error generating serial number")
	}
	return serialNum, nil
}
