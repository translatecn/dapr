package sentry_debug

import (
	"github.com/dapr/dapr/code_debug/replace"
	"github.com/dapr/dapr/pkg/sentry/config"
	"io/ioutil"
	raw_k8s "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"os"
	"path/filepath"
)

func PRE(sentryConfig *config.SentryConfig) {
	if replace.Replace()>0{
		return
	}
	sentryConfig.RootCertPath = "/tmp/ca.crt"
	sentryConfig.IssuerCertPath = "/tmp/issuer.crt"
	sentryConfig.IssuerKeyPath = "/tmp/issuer.key"

	crt := `-----BEGIN CERTIFICATE-----
MIIBxTCCAWqgAwIBAgIQc55uyj2aQwZ44JcP0YBp7DAKBggqhkjOPQQDAjAxMRcw
FQYDVQQKEw5kYXByLmlvL3NlbnRyeTEWMBQGA1UEAxMNY2x1c3Rlci5sb2NhbDAe
Fw0yMTA4MjAwOTUyMjJaFw0yMjA4MjAxMDA3MjJaMBgxFjAUBgNVBAMTDWNsdXN0
ZXIubG9jYWwwWTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAAR75g0xjAr4VcZed62s
xzZ2kUH3Bxatbdq2/5+4hJyMyX7iFNfG1RgJSgfJpTiHXbRbd13Q6Mk+XbY8pZgA
V40Eo30wezAOBgNVHQ8BAf8EBAMCAQYwDwYDVR0TAQH/BAUwAwEB/zAdBgNVHQ4E
FgQUN+JjAulZKw/mBAaPgYhvbukY6KkwHwYDVR0jBBgwFoAUT4gnr9tzfi5hRW9A
9aQF0b5p680wGAYDVR0RBBEwD4INY2x1c3Rlci5sb2NhbDAKBggqhkjOPQQDAgNJ
ADBGAiEA1TtSlfgQXmaQ3rEqt+raaG3QUXWKc6bVuvc8oxQGeQQCIQDCGMgoedRX
w+ZOMIjU2uBQ3QZ/ayy273tQHM/beTgDPQ==
-----END CERTIFICATE-----`
	key := `-----BEGIN EC PRIVATE KEY-----
MHcCAQEEILld+Jm1MDXgVq75SKcgh+wVBYQ/UiYd0TLRoH1wV3P1oAoGCCqGSM49
AwEHoUQDQgAEe+YNMYwK+FXGXnetrMc2dpFB9wcWrW3atv+fuIScjMl+4hTXxtUY
CUoHyaU4h120W3dd0OjJPl22PKWYAFeNBA==
-----END EC PRIVATE KEY-----`
	ca := `-----BEGIN CERTIFICATE-----
MIIB3DCCAYKgAwIBAgIRAJrB3Ct76di0AV9xiwz4RYgwCgYIKoZIzj0EAwIwMTEX
MBUGA1UEChMOZGFwci5pby9zZW50cnkxFjAUBgNVBAMTDWNsdXN0ZXIubG9jYWww
HhcNMjEwODIwMDk1MjIyWhcNMjIwODIwMTAwNzIyWjAxMRcwFQYDVQQKEw5kYXBy
LmlvL3NlbnRyeTEWMBQGA1UEAxMNY2x1c3Rlci5sb2NhbDBZMBMGByqGSM49AgEG
CCqGSM49AwEHA0IABEaGZHa2M60u0BvgAoOn4zqUYr3nRGzKKNhT5f9f4SqlI31V
ftvfJBnOQt6dM3YHGifIUPo782N6MzghkKxeapOjezB5MA4GA1UdDwEB/wQEAwIC
BDAdBgNVHSUEFjAUBggrBgEFBQcDAQYIKwYBBQUHAwIwDwYDVR0TAQH/BAUwAwEB
/zAdBgNVHQ4EFgQUT4gnr9tzfi5hRW9A9aQF0b5p680wGAYDVR0RBBEwD4INY2x1
c3Rlci5sb2NhbDAKBggqhkjOPQQDAgNIADBFAiBscw216OcA8jt9tI1LmTywzNVV
zfCt2fhdjXEK2GGEMAIhAKi0GsyI5b2hkrUkIEZm1kTLbeuw0GIguSvW89yUkXbT
-----END CERTIFICATE-----`

	_ = ioutil.WriteFile(sentryConfig.RootCertPath, []byte(ca), 0644)
	_ = ioutil.WriteFile(sentryConfig.IssuerCertPath, []byte(crt), 0644)
	_ = ioutil.WriteFile(sentryConfig.IssuerKeyPath, []byte(key), 0644)

	os.Setenv("NAMESPACE", "dapr-system")
}

// GetK8s 此处改用加载本地配置文件 ~/.kube/config
func GetK8s() *raw_k8s.Clientset {
	conf, err := rest.InClusterConfig()
	if err != nil {
		// 路径直接写死
		conf, err = clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
		if err != nil {
			panic(err)
		}
	}

	kubeClient, _ := raw_k8s.NewForConfig(conf)
	return kubeClient
}
