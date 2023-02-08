// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package actors

import (
	"sync"
	"time"

	"go.uber.org/atomic"

	"github.com/pkg/errors"

	diag "github.com/dapr/dapr/pkg/diagnostics"
)

// ErrActorDisposed 是当runtime试图持有已经处理的actor的锁时的错误。
var ErrActorDisposed error = errors.New("actor已经处理了")

// 每个actor实例都是    actor_type||actor_id 的具体实例
// actor 代表actor对象，并保持其回合制的并发性。
type actor struct {
	actorType string
	actorID   string

	// actorLock 是维持actor基于回合的并发性的锁，如果配置了重入，则允许重入。
	actorLock ActorLock
	// pendingActorCalls 当前基于回合的阻塞中的数量
	pendingActorCalls atomic.Int32

	// 当一致的散列表被更新时，actor运行时在drainOngoingCallTimeout之后或在所有待定的actor调用完成之前，
	//drainactor以重新平衡各actor主机。 lastUsedTime是最后一次actor调用持有锁的时间。
	//这被用来计算正在进行的调用超时的时间。
	lastUsedTime time.Time

	// disposeLock 处理锁
	disposeLock *sync.RWMutex
	// disposed  当actor已经被处置 为true
	disposed bool
	// disposeCh  当所有待处理的角色调用完成后，发出信号。这个通道在runtime耗尽actor时使用。
	disposeCh chan struct{}

	once sync.Once
}

func newActor(actorType, actorID string, maxReentrancyDepth *int) *actor {
	return &actor{
		actorType:    actorType,
		actorID:      actorID,
		actorLock:    NewActorLock(int32(*maxReentrancyDepth)), // 默认32
		disposeLock:  &sync.RWMutex{},
		disposeCh:    nil,
		disposed:     false,
		lastUsedTime: time.Now().UTC(),
	}
}

// isBusy 判断actor否正在运行
func (a *actor) isBusy() bool {
	a.disposeLock.RLock()
	disposed := a.disposed
	a.disposeLock.RUnlock()
	return !disposed && a.pendingActorCalls.Load() > 0
}

// channel disposeCh是用来释放actor的
func (a *actor) channel() chan struct{} {
	a.once.Do(func() {
		a.disposeLock.Lock()
		a.disposeCh = make(chan struct{})
		a.disposeLock.Unlock()
	})

	a.disposeLock.RLock()
	defer a.disposeLock.RUnlock()
	return a.disposeCh
}

// lock 持有基于回合的并发的锁。
func (a *actor) lock(reentrancyID *string) error {
	pending := a.pendingActorCalls.Inc()
	diag.DefaultMonitoring.ReportActorPendingCalls(a.actorType, pending)

	err := a.actorLock.Lock(reentrancyID)
	if err != nil {
		return err
	}

	a.disposeLock.RLock()
	disposed := a.disposed
	a.disposeLock.RUnlock()
	if disposed {// 判断此actor是否已经被处理掉
		a.unlock()
		return ErrActorDisposed
	}
	a.lastUsedTime = time.Now().UTC()
	return nil
}

// unlock 释放锁，以实现轮流并发。如果disposeCh是可用的，它将关闭通道以通知运行时处置角色。
func (a *actor) unlock() {
	pending := a.pendingActorCalls.Dec()
	if pending == 0 {
		func() {
			a.disposeLock.Lock()
			defer a.disposeLock.Unlock()
			if !a.disposed && a.disposeCh != nil {
				a.disposed = true
				close(a.disposeCh)
			}
		}()
	} else if pending < 0 {
		log.Error("BUGBUG: 试图解锁一个未locking中的锁")
		return
	}

	a.actorLock.Unlock()
	diag.DefaultMonitoring.ReportActorPendingCalls(a.actorType, pending)
}
