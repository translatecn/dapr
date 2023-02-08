// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package actors

// CreateReminderRequest 是创建一个新的提醒的请求对象。
type CreateReminderRequest struct {
	Name      string
	ActorType string
	ActorID   string
	Data      interface{} `json:"data"`
	DueTime   string      `json:"dueTime"`// 到期、启动、开始时间
	Period    string      `json:"period"`
	TTL       string      `json:"ttl"`
}
