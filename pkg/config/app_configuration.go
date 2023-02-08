// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package config

// ApplicationConfig 应用程序提供的配置
type ApplicationConfig struct {
	Entities []string `json:"entities"` // 实体
	// Duration. example: "1h" Actor空闲时间
	ActorIdleTimeout string `json:"actorIdleTimeout"`
	// Duration. example: "30s" Actor扫描间隔
	ActorScanInterval string `json:"actorScanInterval"`
	// Duration. example: "30s" 调用超时
	DrainOngoingCallTimeout    string           `json:"drainOngoingCallTimeout"`
	DrainRebalancedActors      bool             `json:"drainRebalancedActors"` // 重新平衡Actor
	Reentrancy                 ReentrancyConfig `json:"reentrancy,omitempty"`
	RemindersStoragePartitions int              `json:"remindersStoragePartitions"` // 提醒者 存储分区
}

type ReentrancyConfig struct {
	Enabled       bool `json:"enabled"`
	MaxStackDepth *int `json:"maxStackDepth,omitempty"` // 最大栈深度
}
