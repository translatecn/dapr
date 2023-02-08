// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package raft

import (
	"github.com/hashicorp/raft"
)

// snapshot 是用来提供当前状态的快照，可以与可能修改实时状态的操作同时访问。可能修改实时状态的操作同时访问。
type snapshot struct {
	state *DaprHostMemberState
}

// Persist 将FSM快照保存到给定的目标中。
func (s *snapshot) Persist(sink raft.SnapshotSink) error {
	if err := s.state.persist(sink); err != nil {
		sink.Cancel()
	}

	return sink.Close()
}

// Release 释放状态存储资源。No-Ops，因为我们使用 在内存中的状态。
func (s *snapshot) Release() {}

var _ raft.FSMSnapshot = &snapshot{}
