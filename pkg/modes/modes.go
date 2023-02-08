// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package modes

// DaprMode 运行模式
type DaprMode string

const (
	// KubernetesMode K8S集群模式
	KubernetesMode DaprMode = "kubernetes"
	// StandaloneMode 单机模式
	StandaloneMode DaprMode = "standalone"
)
