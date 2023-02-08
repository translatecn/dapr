package kubernetes

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// GetClient 在k8s环境中中调用
func GetClient() (*kubernetes.Clientset, error) {
	//返回一个配置对象，该对象使用kubernetes提供给pod的服务账户。
	//它是为那些希望在运行在kubernetes上的pod内运行的客户准备的。
	//如果从一个不在kubernetes环境中运行的进程中调用，它将返回ErrNotInCluster。
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}
