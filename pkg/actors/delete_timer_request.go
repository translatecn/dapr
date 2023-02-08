// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package actors

// DeleteTimerRequest 是删除一个timer的请求对象
type DeleteTimerRequest struct {
	Name      string
	ActorType string
	ActorID   string
}
