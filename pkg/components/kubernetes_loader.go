// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package components

import (
	"context"
	"encoding/json"
	"time"

	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"

	"github.com/dapr/kit/logger"

	components_v1alpha1 "github.com/dapr/dapr/pkg/apis/components/v1alpha1"
	config "github.com/dapr/dapr/pkg/config/modes"
	operatorv1pb "github.com/dapr/dapr/pkg/proto/operator/v1"
)

var log = logger.NewLogger("dapr.runtime.components")

const (
	operatorCallTimeout = time.Second * 5
	operatorMaxRetries  = 100
)

// KubernetesComponents 在kubernetes环境中加载组件。
type KubernetesComponents struct {
	config    config.KubernetesConfig
	client    operatorv1pb.OperatorClient
	namespace string
}

// NewKubernetesComponents 返回k8s组件的加载器  【ControlPlaneAddress ，127.0.0.1：6500】
// mesoid, ControlPlaneAddress连接的客户端
func NewKubernetesComponents(configuration config.KubernetesConfig, namespace string, operatorClient operatorv1pb.OperatorClient) *KubernetesComponents {
	return &KubernetesComponents{
		config:    configuration,
		client:    operatorClient,
		namespace: namespace,
	}
}

// LoadComponents // 返回一个给定控制面地址的客户端。 从operator加载组件
func (k *KubernetesComponents) LoadComponents() ([]components_v1alpha1.Component, error) {
	// 获取所有自定义资源
	// dapr.io/Components
	// operator 客户端
	resp, err := k.client.ListComponents(
		context.Background(),
		&operatorv1pb.ListComponentsRequest{
			Namespace: k.namespace,
		},
		grpc_retry.WithMax(operatorMaxRetries),
		grpc_retry.WithPerRetryTimeout(operatorCallTimeout),
	)
	if err != nil {
		return nil, err
	}
	comps := resp.GetComponents()

	components := []components_v1alpha1.Component{}
	for _, c := range comps {
		var component components_v1alpha1.Component
		component.Spec = components_v1alpha1.ComponentSpec{}
		err := json.Unmarshal(c, &component)
		if err != nil {
			log.Warnf("error deserializing component: %s", err)
			continue
		}
		components = append(components, component)
	}
	return components, nil
}
//apiVersion: dapr.io/v1alpha1
//kind: Component
//metadata:
//  name: statestore
//  namespace: liushuo
//  uid: 91d1b0a0-684f-4f1e-814d-8240bc7f24f3
//spec:
//  metadata:
//    - name: redisHost
//      value: 'redis:6379'
//    - name: redisPassword
//      value: ''
//  type: state.redis
//  version: v1
//auth:
//  secretStore: kubernetes
