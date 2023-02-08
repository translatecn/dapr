// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package version

// 以下几个值，构建时注入
var (
	version = "edge"

	gitcommit, gitversion string
)

// Version 返回 dapr版本
func Version() string {
	return version
}

// Commit 返回git commit sha
func Commit() string {
	return gitcommit
}

// GitVersion 返回git 版本
func GitVersion() string {
	return gitversion
}
