// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package raft

import (
	"io"
	"strconv"
	"sync"

	"github.com/hashicorp/raft"
	"github.com/pkg/errors"

	v1pb "github.com/dapr/dapr/pkg/proto/placement/v1"
)

// CommandType raft 命令类型
type CommandType uint8

const (
	// MemberUpsert 更新、插入成员信息
	MemberUpsert CommandType = 0
	// MemberRemove 从actor主机成员状态中删除成员的命令。
	MemberRemove CommandType = 1

	// TableDisseminate 是为传播环路保留的命令。
	TableDisseminate CommandType = 100
)

// FSM提供了一个接口，可以由客户端实现使用复制的日志。
//type FSM interface {
//		一旦提交了日志条目，就会调用Apply log。它返回的值将在
//		Raft返回的ApplyFuture。应用方法方法在与FSM相同的Raft节点上调用。
//	Apply(*Log) interface{}
//		快照用于支持日志压缩。这个电话应该返回一个可以用来保存时间点的FSMSnapshot
//		FSM的快照。Apply和Snapshot不会被多次调用线程，但Apply将与Persist同时调用。这意味着
//		FSM应该以允许并发的方式实现快照发生时进行更新。
//	Snapshot() (FSMSnapshot, error)
//		Restore用于从快照中恢复FSM。它不叫与任何其他命令同时执行。FSM必须丢弃之前的所有内容状态。
//	Restore(io.ReadCloser) error
//}

// FSM 实现了一个有限状态机，与Raft一起使用，以提供强一致性。我们在服务器之外实现它，以避免将其暴露在包之外。
type FSM struct {
	//stateLock仅用于保护State()的外部调用者不与Restore()竞争，后者由Raft调用(它放入一个全新的状态存储)。
	//这里的所有内部内容都是由Raft端同步的，所以不需要锁这个。
	stateLock sync.RWMutex
	state     *DaprHostMemberState // dapr成员状态
}

func newFSM() *FSM {
	return &FSM{
		state: newDaprHostMemberState(),
	}
}

// State 用于返回当前状态的句柄。
func (c *FSM) State() *DaprHostMemberState {
	c.stateLock.RLock()
	defer c.stateLock.RUnlock()
	return c.state
}

// PlacementState 返回当前的放置表
func (c *FSM) PlacementState() *v1pb.PlacementTables {
	c.stateLock.RLock()
	defer c.stateLock.RUnlock()

	newTable := &v1pb.PlacementTables{
		Version: strconv.FormatUint(c.state.TableGeneration(), 10),
		Entries: make(map[string]*v1pb.PlacementTable),
	}

	totalHostSize := 0
	totalSortedSet := 0
	totalLoadMap := 0

	entries := c.state.hashingTableMap()
	//TODO 再遍历过程中发生了变化
	for k, v := range entries {
		hosts, sortedSet, loadMap, totalLoad := v.GetInternals()
		table := v1pb.PlacementTable{
			Hosts:     make(map[uint64]string),
			SortedSet: make([]uint64, len(sortedSet)),
			TotalLoad: totalLoad,
			LoadMap:   make(map[string]*v1pb.Host),
		}

		for lk, lv := range hosts {
			table.Hosts[lk] = lv
		}

		copy(table.SortedSet, sortedSet)

		for lk, lv := range loadMap {
			h := v1pb.Host{
				Name: lv.Name,
				Load: lv.Load,
				Port: lv.Port,
				Id:   lv.AppID,
			}
			table.LoadMap[lk] = &h
		}
		newTable.Entries[k] = &table

		totalHostSize += len(table.Hosts)
		totalSortedSet += len(table.SortedSet)
		totalLoadMap += len(table.LoadMap)
	}

	logging.Debugf("PlacementTable 大小, 主机数: %d, 排序集: %d, LoadMap: %d", totalHostSize, totalSortedSet, totalLoadMap)

	return newTable
}
// OK
func (c *FSM) upsertMember(cmdData []byte) (bool, error) {
	var host DaprHostMember
	if err := unmarshalMsgPack(cmdData, &host); err != nil {
		return false, err
	}

	c.stateLock.RLock()
	defer c.stateLock.RUnlock()

	return c.state.upsertMember(&host), nil
}
// OK
func (c *FSM) removeMember(cmdData []byte) (bool, error) {
	var host DaprHostMember
	if err := unmarshalMsgPack(cmdData, &host); err != nil {
		return false, err
	}

	c.stateLock.RLock()
	defer c.stateLock.RUnlock()

	return c.state.removeMember(&host), nil
}

var _ raft.FSM = &FSM{}

//Apply(*Log) interface{} 执行一条日志
//Snapshot() (FSMSnapshot, error)// 返回一个快照
//Restore(io.ReadCloser) error//

// Apply 当提交日志时，就会触发
func (c *FSM) Apply(log *raft.Log) interface{} {
	var (
		err     error
		updated bool
	)

	if log.Index < c.state.Index() {
		logging.Warnf("old: %d, new index: %d. skip apply", c.state.Index, log.Index)
		return false
	}

	switch CommandType(log.Data[0]) {
	case MemberUpsert:
		updated, err = c.upsertMember(log.Data[1:])
	case MemberRemove:
		updated, err = c.removeMember(log.Data[1:])
	default:
		err = errors.New("unimplemented command")
	}

	if err != nil {
		logging.Errorf("fsm apply entry log failed. data: %s, error: %s",
			string(log.Data), err.Error())
		return false
	}

	return updated
}

// Snapshot
// 用于支持日志压缩。这个调佣应该返回一个FSMSnapshot，它可以用来保存一个时间点 FSM的快照。
func (c *FSM) Snapshot() (raft.FSMSnapshot, error) {
	return &snapshot{
		state: c.state.clone(),
	}, nil
}

// Restore 恢复快照中的流，并根据快照替换当前的状态存储，如果恢复过程中一切正常的话。
func (c *FSM) Restore(old io.ReadCloser) error {
	defer old.Close()

	members := newDaprHostMemberState()
	if err := members.restore(old); err != nil {
		return err
	}

	c.stateLock.Lock()
	c.state = members
	c.stateLock.Unlock()

	return nil
}
