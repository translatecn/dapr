// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package metrics

import (
	"github.com/dapr/dapr/utils"
	"strconv"
)

const (
	//defaultMetricsPort    = "9090"
	defaultMetricsEnabled = true
)

var defaultMetricsPort, _ = utils.GetAvailablePort()

// Options 定义用于Dapr日志记录的选项集。
type Options struct {
	// OutputLevel是日志级别
	MetricsEnabled bool

	Port string
}

func defaultMetricOptions() *Options {
	return &Options{
		Port:           defaultMetricsPort,
		MetricsEnabled: defaultMetricsEnabled,
	}
}

// MetricsPort 获取指标的端口
func (o *Options) MetricsPort() uint64 {
	port, err := strconv.ParseUint(o.Port, 10, 64)
	if err != nil {
		// Use default metrics port as a fallback
		port, _ = strconv.ParseUint(defaultMetricsPort, 10, 64)
	}

	return port
}

// AttachCmdFlags attaches metrics options to command flags.
// 交由外部统一parse
func (o *Options) AttachCmdFlags(
	stringVar func(p *string, name string, value string, usage string),
	boolVar func(p *bool, name string, value bool, usage string)) {
	stringVar(
		&o.Port,
		"metrics-port",
		defaultMetricsPort,
		"The port for the metrics server")
	boolVar(
		&o.MetricsEnabled,
		"enable-metrics",
		defaultMetricsEnabled,
		"Enable prometheus metric")
}

// AttachCmdFlag attaches single metrics option to command flags.
func (o *Options) AttachCmdFlag(
	stringVar func(p *string, name string, value string, usage string)) {
	stringVar(
		&o.Port,
		"metrics-port",
		defaultMetricsPort,
		"The port for the metrics server")
}
