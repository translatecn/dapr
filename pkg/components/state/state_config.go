// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package state

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
)

const (
	strategyKey = "keyPrefix"

	strategyAppid     = "appid"
	strategyStoreName = "name"
	strategyNone      = "none"
	strategyDefault   = strategyAppid

	daprSeparator = "||"
)

// 全局对象   ，存储实例
var statesConfiguration = map[string]*StoreConfiguration{}

type StoreConfiguration struct {
	keyPrefixStrategy string // k,v存储 , 默认给key 添加一个前缀
}

// SaveStateConfiguration 保存状态配置
func SaveStateConfiguration(storeName string, metadata map[string]string) error {
	strategy := metadata[strategyKey] // keyPrefix
	strategy = strings.ToLower(strategy)
	if strategy == "" {
		strategy = strategyDefault // appid
	} else {
		err := checkKeyIllegal(metadata[strategyKey])
		if err != nil {
			return err
		}
	}
	//                   redis-statestore
	statesConfiguration[storeName] = &StoreConfiguration{keyPrefixStrategy: strategy}
	return nil
}

//GetModifiedStateKey 获取修改后的状态键
func GetModifiedStateKey(key, storeName, appID string) (string, error) {
	if err := checkKeyIllegal(key); err != nil {
		return "", err
	}
	stateConfiguration := getStateConfiguration(storeName) // 从全局对象中获取的
	switch stateConfiguration.keyPrefixStrategy {          // appid
	case strategyNone: // none
		return key, nil
	case strategyStoreName: // name
		return fmt.Sprintf("%s%s%s", storeName, daprSeparator, key), nil // x||key
	case strategyAppid: // appid ，默认是此值
		if appID == "" {
			return key, nil
		}
		return fmt.Sprintf("%s%s%s", appID, daprSeparator, key), nil
	default:
		return fmt.Sprintf("%s%s%s", stateConfiguration.keyPrefixStrategy, daprSeparator, key), nil
	}
}

// GetOriginalStateKey 获取dapr加工前的key
func GetOriginalStateKey(modifiedStateKey string) string {
	splits := strings.Split(modifiedStateKey, daprSeparator)
	if len(splits) <= 1 {
		return modifiedStateKey
	}
	return splits[1]
}

func getStateConfiguration(storeName string) *StoreConfiguration {
	c := statesConfiguration[storeName]
	if c == nil {
		c = &StoreConfiguration{keyPrefixStrategy: strategyDefault}
		statesConfiguration[storeName] = c
	}

	return c
}

// 检查包不包含 ||
func checkKeyIllegal(key string) error {
	if strings.Contains(key, daprSeparator) {
		return errors.Errorf("input key/keyPrefix '%s' can't contain '%s'", key, daprSeparator)
	}
	return nil
}
