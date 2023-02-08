// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package actors

// CreateTimerRequest 是创建新定时器的请求对象。
type CreateTimerRequest struct {
	Name      string
	ActorType string
	ActorID   string
	DueTime   string      `json:"dueTime"`  // 到期时间
	Period    string      `json:"period"`   // 周期
	TTL       string      `json:"ttl"`      // 生存时间
	Callback  string      `json:"callback"` // 回调
	Data      interface{} `json:"data"`     // 具体数据
}
