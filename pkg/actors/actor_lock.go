// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package actors

import (
	"sync"

	"github.com/pkg/errors"
	"go.uber.org/atomic"
)

var ErrMaxStackDepthExceeded error = errors.New("超过了最大栈深度") // 这里感觉就是限制了并发

type ActorLock struct {
	methodLock    *sync.Mutex   // 方法锁
	requestLock   *sync.Mutex   // 请求锁
	activeRequest *string       // 活跃请求
	stackDepth    *atomic.Int32 // 栈深度 原子操作
	maxStackDepth int32         // 最大栈深度
}

func NewActorLock(maxStackDepth int32) ActorLock {
	return ActorLock{
		methodLock:    &sync.Mutex{},
		requestLock:   &sync.Mutex{},
		activeRequest: nil,
		stackDepth:    atomic.NewInt32(int32(0)),
		maxStackDepth: maxStackDepth,
	}
}

// Lock 给方法加锁
func (a *ActorLock) Lock(requestID *string) error { // reentrancyID
	// 获取当前获取请求
	currentRequest := a.getCurrentID()
	// 判断有没继续增加栈深的可能性
	if a.stackDepth.Load() == a.maxStackDepth {
		return ErrMaxStackDepthExceeded
	}

	if currentRequest == nil || *currentRequest != *requestID {
		a.methodLock.Lock()
		a.setCurrentID(requestID)
		a.stackDepth.Inc()
	} else {
		a.stackDepth.Inc()
	}

	return nil
}

// Unlock 给方法解锁
func (a *ActorLock) Unlock() {
	a.stackDepth.Dec()
	//如果当前已经没有调用了,则清空请求ID
	if a.stackDepth.Load() == 0 {
		a.clearCurrentID()
		// TODO 为啥==0时,才会解锁
		a.methodLock.Unlock()
	}
}

// 获取当前活跃请求
func (a *ActorLock) getCurrentID() *string {
	a.requestLock.Lock()
	defer a.requestLock.Unlock()

	return a.activeRequest
}

// 设置活跃请求
func (a *ActorLock) setCurrentID(id *string) {
	a.requestLock.Lock()
	defer a.requestLock.Unlock()

	a.activeRequest = id
}

// 置空活跃请求
func (a *ActorLock) clearCurrentID() {
	a.requestLock.Lock()
	defer a.requestLock.Unlock()

	a.activeRequest = nil
}
