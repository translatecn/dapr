// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package components

import "strings"


// IsInitialVersion 当一个版本被认为是不稳定版本（v0）或第一个稳定版本（v1）时，返回true。为了向后兼容，也包括空字符串。
// 是不是初始版本
func IsInitialVersion(version string) bool {
	//小写
	v := strings.ToLower(version)
	return v == "" || v == UnstableVersion || v == FirstStableVersion
}

const (
	// UnstableVersion 不稳定版本 version (v0).
	UnstableVersion = "v0"

	// FirstStableVersion 第一个稳定版本 (v1).
	FirstStableVersion = "v1"
)
