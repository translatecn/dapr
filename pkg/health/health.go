// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package health

import (
	"time"

	"github.com/valyala/fasthttp"
)

const (
	initialDelay      = time.Second * 1
	failureThreshold  = 2
	requestTimeout    = time.Second * 2
	interval          = time.Second * 5
	successStatusCode = 200
)

// Option 健康检查函数扩展
type Option func(o *healthCheckOptions)

type healthCheckOptions struct {
	initialDelay      time.Duration
	requestTimeout    time.Duration
	failureThreshold  int
	interval          time.Duration
	successStatusCode int
}

// StartEndpointHealthCheck 健康检查成功发送true ,失败发送false
func StartEndpointHealthCheck(endpointAddress string, opts ...Option) chan bool {
	options := &healthCheckOptions{}
	applyDefaults(options)

	for _, o := range opts {
		o(options)
	}
	signalChan := make(chan bool, 1)

	go func(ch chan<- bool, endpointAddress string, options *healthCheckOptions) {
		// 检查周期定时器
		ticker := time.NewTicker(options.interval)
		failureCount := 0
		time.Sleep(options.initialDelay)

		client := &fasthttp.Client{
			MaxConnsPerHost:           5, // Limit Keep-Alive connections
			ReadTimeout:               options.requestTimeout,
			MaxIdemponentCallAttempts: 1,
		}

		req := fasthttp.AcquireRequest() // 体现了fast http的复用
		req.SetRequestURI(endpointAddress)
		req.Header.SetMethod(fasthttp.MethodGet)
		defer fasthttp.ReleaseRequest(req)

		for range ticker.C {
			resp := fasthttp.AcquireResponse()
			err := client.DoTimeout(req, resp, options.requestTimeout)
			if err != nil || resp.StatusCode() != options.successStatusCode {
				failureCount++
				if failureCount == options.failureThreshold {
					failureCount--
					ch <- false
				}
			} else {
				ch <- true
				failureCount = 0
			}
			fasthttp.ReleaseResponse(resp)
		}
	}(signalChan, endpointAddress, options)
	return signalChan
}

func applyDefaults(o *healthCheckOptions) {
	o.failureThreshold = failureThreshold
	o.initialDelay = initialDelay
	o.requestTimeout = requestTimeout
	o.successStatusCode = successStatusCode
	o.interval = interval
}

// WithInitialDelay 健康检查初始延迟时间
func WithInitialDelay(delay time.Duration) Option {
	return func(o *healthCheckOptions) {
		o.initialDelay = delay
	}
}

// WithFailureThreshold 健康检查失败次数
func WithFailureThreshold(threshold int) Option {
	return func(o *healthCheckOptions) {
		o.failureThreshold = threshold
	}
}

// WithRequestTimeout 健康检查超时时间
func WithRequestTimeout(timeout time.Duration) Option {
	return func(o *healthCheckOptions) {
		o.requestTimeout = timeout
	}
}

// WithSuccessStatusCode 设置健康检查成功的状态
func WithSuccessStatusCode(code int) Option {
	return func(o *healthCheckOptions) {
		o.successStatusCode = code
	}
}

// WithInterval 健康检查周期
func WithInterval(interval time.Duration) Option {
	return func(o *healthCheckOptions) {
		o.interval = interval
	}
}
