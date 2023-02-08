// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package v1alpha1

import (
	"strconv"

	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:noStatus
// +kubebuilder:object:root=true

// Component dapr组件的描述
type Component struct {
	metav1.TypeMeta `json:",inline"` // 这两个字段是k8s资源自己定义的
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`// 这两个字段是k8s资源自己定义的
	// +optional
	Spec ComponentSpec `json:"spec,omitempty"`
	// +optional
	Auth `json:"auth,omitempty"`
	// +optional
	Scopes []string `json:"scopes,omitempty"`
}

// ComponentSpec 组件的定义
type ComponentSpec struct {
	Type    string `json:"type"` // 组件类型，   :   大类型.组件实现
	Version string `json:"version"` // 版本
	// +optional
	IgnoreErrors bool           `json:"ignoreErrors"`
	Metadata     []MetadataItem `json:"metadata"` // 用户自己设置的键值对,可以引用k8s里secret的值
	// +optional
	InitTimeout string `json:"initTimeout"`
}

// MetadataItem 是一个元数据的名/值对。
type MetadataItem struct {
	Name string `json:"name"`
	// +optional
	Value DynamicValue `json:"value,omitempty"`
	// +optional
	SecretKeyRef SecretKeyRef `json:"secretKeyRef,omitempty"`  // k8s中的引用对象
}

// SecretKeyRef 是对持有元数据项值的秘密的引用。Name是秘密的名称，key是秘密中的字段。
type SecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// Auth 代表该组件的认证细节。
type Auth struct {
	SecretStore string `json:"secretStore"`
}

// +kubebuilder:object:root=true

// ComponentList is a list of Dapr components.
type ComponentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Component `json:"items"`
}

// DynamicValue is a dynamic value struct for the component.metadata pair value.
type DynamicValue struct {
	v1.JSON `json:",inline"`
}

// String returns the string representation of the raw value.
// If the value is a string, it will be unquoted as the string is guaranteed to be a JSON serialized string.
func (d *DynamicValue) String() string {
	s := string(d.Raw)
	c, err := strconv.Unquote(s)
	if err == nil {
		s = c
	}
	return s
}
