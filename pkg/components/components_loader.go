// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package components

import components_v1alpha1 "github.com/dapr/dapr/pkg/apis/components/v1alpha1"

// ComponentLoader 是返回Dapr组件的接口。
type ComponentLoader interface {
	LoadComponents() ([]components_v1alpha1.Component, error)
}
