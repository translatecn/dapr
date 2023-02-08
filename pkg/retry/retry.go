// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package retry

import "time"

const (
	DefaultLinearBackoffInterval = time.Second // 默认的线性回退间隔
	DefaultLinearRetryCount      = 3
)
