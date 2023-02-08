// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package actors

// DeleteReminderRequest 是删除一个reminder的请求对象。
type DeleteReminderRequest struct {
	Name      string
	ActorType string
	ActorID   string
}
