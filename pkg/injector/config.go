// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package injector

import (
	"github.com/kelseyhightower/envconfig"

	"github.com/dapr/dapr/utils"
)

// Config 表示Dapr Sidecar Injector webhook服务器的配置选项。
type Config struct {
	TLSCertFile            string `envconfig:"TLS_CERT_FILE" required:"true"`
	TLSKeyFile             string `envconfig:"TLS_KEY_FILE" required:"true"`
	SidecarImage           string `envconfig:"SIDECAR_IMAGE" required:"true"`
	SidecarImagePullPolicy string `envconfig:"SIDECAR_IMAGE_PULL_POLICY"`
	Namespace              string `envconfig:"NAMESPACE" required:"true"`
	KubeClusterDomain      string `envconfig:"KUBE_CLUSTER_DOMAIN"`
}

// NewConfigWithDefaults 返回已应用默认值的Config对象。然后调用者可以自由地为其余字段设置自定义值和/或覆盖默认值。
func NewConfigWithDefaults() Config {
	return Config{
		SidecarImagePullPolicy: "Always",
	}
}

// GetConfig 返回从环境变量派生的配置。
func GetConfig() (Config, error) {
	// 从环境变量获取配置
	c := NewConfigWithDefaults()
	// 将环境变量转换为结构体
	err := envconfig.Process("", &c)
	if err != nil {
		return c, err
	}

	if c.KubeClusterDomain == "" {
		// 从resolv.conf文件中自动检测KubeClusterDomain
		clusterDomain, err := utils.GetKubeClusterDomain()
		if err != nil {
			log.Errorf("failed to get clusterDomain err:%s, set default:%s", err, utils.DefaultKubeClusterDomain)
			c.KubeClusterDomain = utils.DefaultKubeClusterDomain
		} else {
			c.KubeClusterDomain = clusterDomain
		}
	}
	return c, nil
}
