// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package components

import components_v1alpha1 "github.com/dapr/dapr/pkg/apis/components/v1alpha1"

// ComponentHandler 是对组件变化作出反应的接口。
type ComponentHandler interface {
	OnComponentUpdated(component components_v1alpha1.Component)
}
