// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

// Package concurrency CODE ATTRIBUTION: https://github.com/korovkin/limiter
// Modified to accept a parameter to the executed job
package concurrency

import (
	"sync/atomic"
)

const (
	// DefaultLimit 并发限制
	DefaultLimit = 100
)

// Limiter object.
type Limiter struct {
	limit         int      // 并发数
	tickets       chan int // 并发队列,类似于漏桶
	numInProgress int32    // 处理中的任务
}

// NewLimiter 分配一个新的并发限制器。
func NewLimiter(limit int) *Limiter {
	if limit <= 0 {
		limit = DefaultLimit
	}
	c := &Limiter{
		limit:   limit,
		tickets: make(chan int, limit),
	}

	// 填充🔑
	for i := 0; i < c.limit; i++ {
		c.tickets <- i
	}

	return c
}

// Execute Execute将一个函数添加到执行队列中，如果这个实例分配的go routine数量< limit，
//则启动一个新的go routine来执行作业，否则等待一个go routine可用。
func (c *Limiter) Execute(job func(param interface{}), param interface{}) int {
	//同一时刻,最多有limit个任务在运行
	ticket := <-c.tickets
	atomic.AddInt32(&c.numInProgress, 1)
	go func(param interface{}) {
		defer func() {
			c.tickets <- ticket
			atomic.AddInt32(&c.numInProgress, -1)
		}()

		job(param)
	}(param)
	return ticket
}

// Wait 阻塞知道之前所有的任务完成.
// IMPORTANT: 在不断调用Execute的同时调用Wait函数会导致不期望的竞争条件
func (c *Limiter) Wait() {
	for i := 0; i < c.limit; i++ {
		<-c.tickets
	}
}

// GetNumInProgress 返回一个计数器，显示现在有多少个go程序在活动。
func (c *Limiter) GetNumInProgress() int32 {
	return atomic.LoadInt32(&c.numInProgress)
}
func Test() {
	limit := NewLimiter(10)
	for i := 0; i < 1000; i++ {
		limit.Execute(func(param interface{}) {

		}, "")
	}
	limit.Wait()
}
