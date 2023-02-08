// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package diagnostics

import (
	"time"

	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

// appIDKey 类型转换
var appIDKey = tag.MustNewKey("app_id")

var (
	// DefaultReportingPeriod 汇报周期
	DefaultReportingPeriod = 1 * time.Minute

	// DefaultMonitoring  持有服务监控指标的定义。
	DefaultMonitoring = newServiceMetrics()
	// DefaultGRPCMonitoring 持有默认的gRPC监控处理程序和中间件。
	DefaultGRPCMonitoring = newGRPCMetrics()
	// DefaultHTTPMonitoring 持有默认的HTTP监控处理程序和中间件。
	DefaultHTTPMonitoring = newHTTPMetrics()
)

// InitMetrics 初始化指标
func InitMetrics(appID string) error {
	if err := DefaultMonitoring.Init(appID); err != nil {
		return err
	}

	if err := DefaultGRPCMonitoring.Init(appID); err != nil {
		return err
	}

	if err := DefaultHTTPMonitoring.Init(appID); err != nil {
		return err
	}

	// 设置汇报周期
	view.SetReportingPeriod(DefaultReportingPeriod)

	return nil
}
