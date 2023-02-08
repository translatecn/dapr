package credentials

import (
	"os"
)

const (
	// RootCertFilename 根证书的名字
	RootCertFilename = "ca.crt"
	// IssuerCertFilename 持有颁发者证书的文件名。
	IssuerCertFilename = "issuer.crt"
	// IssuerKeyFilename 保存颁发者密钥的文件名。
	IssuerKeyFilename = "issuer.key"
)

// CertChain 保存证书信任链PEM值。 Openssl使用PEM(RFC 1421－1424)文档格式
type CertChain struct {
	RootCA []byte
	Cert   []byte
	Key    []byte
}

// LoadFromDisk 从给定目录返回CertChain。
func LoadFromDisk(rootCertPath, issuerCertPath, issuerKeyPath string) (*CertChain, error) {
	rootCert, err := os.ReadFile(rootCertPath)
	if err != nil {
		return nil, err
	}
	cert, err := os.ReadFile(issuerCertPath)
	if err != nil {
		return nil, err
	}
	key, err := os.ReadFile(issuerKeyPath)
	if err != nil {
		return nil, err
	}
	return &CertChain{
		RootCA: rootCert,
		Cert:   cert,
		Key:    key,
	}, nil
}
