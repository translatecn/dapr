package credentials

import (
	"path/filepath"
)

// TLSCredentials 证书保存路径
type TLSCredentials struct {
	credentialsPath string
}

// NewTLSCredentials 返回 TLSCredentials.
func NewTLSCredentials(path string) TLSCredentials {
	return TLSCredentials{
		credentialsPath: path,
	}
}

// Path 返回 证书文件夹路径
func (t *TLSCredentials) Path() string {
	return t.credentialsPath
}

// RootCertPath 返回 根证书路径
func (t *TLSCredentials) RootCertPath() string {
	return filepath.Join(t.credentialsPath, RootCertFilename)
}

// CertPath  返回 用户证书路径
func (t *TLSCredentials) CertPath() string {
	return filepath.Join(t.credentialsPath, IssuerCertFilename)
}

// KeyPath 返回用户私钥路径
func (t *TLSCredentials) KeyPath() string {
	return filepath.Join(t.credentialsPath, IssuerKeyFilename)
}
