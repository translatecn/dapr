// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package raft

import (
	"net"
	"path/filepath"
	"time"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
	"github.com/pkg/errors"
)

const (
	logStorePrefix    = "log-"
	snapshotsRetained = 2 // 快照数量

	// raftLogCacheSize 是要在内存中缓存的日志的最大数量。 这是用来减少最近提交的条目的磁盘I/O。
	raftLogCacheSize = 512

	commandTimeout = 1 * time.Second

	nameResolveRetryInterval = 2 * time.Second
	nameResolveMaxRetry      = 120
)

// PeerInfo 代表 "raft "每个节点信息。
type PeerInfo struct {
	ID      string
	Address string
}

// Server raft协议的实现
type Server struct {
	id  string
	fsm *FSM

	inMem    bool
	raftBind string
	peers    []PeerInfo

	config        *raft.Config
	raft          *raft.Raft
	raftStore     *raftboltdb.BoltStore
	raftTransport *raft.NetworkTransport

	logStore    raft.LogStore
	stableStore raft.StableStore
	snapStore   raft.SnapshotStore

	raftLogStorePath string
}

// New 创建raft服务 节点
func New(id string, inMem bool, peers []PeerInfo, logStorePath string) *Server {
	raftBind := raftAddressForID(id, peers)
	if raftBind == "" {
		return nil
	}

	return &Server{
		id:               id,
		inMem:            inMem,
		raftBind:         raftBind,
		peers:            peers,
		raftLogStorePath: logStorePath,
	}
}

// 尝试解析raft地址
func tryResolveRaftAdvertiseAddr(bindAddr string) (*net.TCPAddr, error) {
	//HACKHACK：Kubernetes POD的DNS A记录 需要等一些时间
	// 才能在StatefulSet POD部署后需要一些时间来查询地址。
	var err error
	var addr *net.TCPAddr
	for retry := 0; retry < nameResolveMaxRetry; retry++ {
		addr, err = net.ResolveTCPAddr("tcp", bindAddr)
		if err == nil {
			return addr, nil
		}
		time.Sleep(nameResolveRetryInterval)
	}
	return nil, err
}

// StartRaft 启动raft服务
func (s *Server) StartRaft(config *raft.Config) error {
	defer func() {
		if s.raft == nil && s.raftStore != nil {
			if err := s.raftStore.Close(); err != nil {
				logging.Errorf("failed to close log storage: %v", err)
			}
		}
	}()

	s.fsm = newFSM()

	addr, err := tryResolveRaftAdvertiseAddr(s.raftBind)
	if err != nil {
		return err
	}

	loggerAdapter := newLoggerAdapter()
	trans, err := raft.NewTCPTransportWithLogger(s.raftBind, addr, 3, 10*time.Second, loggerAdapter)
	if err != nil {
		return err
	}

	s.raftTransport = trans

	// Build an all in-memory setup for dev mode, otherwise prepare a full
	// disk-based setup.
	// 为开发模式建立一个全内存的设置，否则准备一个基于磁盘的完整设置。
	if s.inMem {
		raftInmem := raft.NewInmemStore()
		s.stableStore = raftInmem
		s.logStore = raftInmem
		s.snapStore = raft.NewInmemSnapshotStore()
	} else {
		if err = ensureDir(s.raftStorePath()); err != nil {
			return errors.Wrap(err, "failed to create log store directory")
		}
		// 创建存储
		s.raftStore, err = raftboltdb.NewBoltStore(filepath.Join(s.raftStorePath(), "raft.db"))
		if err != nil {
			return err
		}
		s.stableStore = s.raftStore

		// 包装LogCache存储，用于提升性能
		s.logStore, err = raft.NewLogCache(raftLogCacheSize, s.raftStore)
		if err != nil {
			return err
		}

		// 创建快照存储
		s.snapStore, err = raft.NewFileSnapshotStoreWithLogger(s.raftStorePath(), snapshotsRetained, loggerAdapter)
		if err != nil {
			return err
		}
	}

	if config == nil {
		// raft默认配置
		s.config = &raft.Config{
			ProtocolVersion:    raft.ProtocolVersionMax,
			HeartbeatTimeout:   1000 * time.Millisecond,
			ElectionTimeout:    1000 * time.Millisecond,
			CommitTimeout:      50 * time.Millisecond,
			MaxAppendEntries:   64,
			ShutdownOnRemove:   true,
			TrailingLogs:       10240,
			SnapshotInterval:   120 * time.Second,
			SnapshotThreshold:  8192,
			LeaderLeaseTimeout: 500 * time.Millisecond,
		}
	} else {
		s.config = config
	}

	// 使用LoggerAdapter与Dapr日志器集成。日志级别依赖于放置日志级别。
	s.config.Logger = loggerAdapter
	s.config.LocalID = raft.ServerID(s.id)

	// 如果我们处于引导或开发模式，并且状态是干净的，那么我们现在就可以引导了。
	bootstrapConf, err := s.bootstrapConfig(s.peers)
	if err != nil {
		return err
	}

	if bootstrapConf != nil {
		if err = raft.BootstrapCluster(
			s.config, s.logStore, s.stableStore,
			s.snapStore, trans, *bootstrapConf); err != nil {
			return err
		}
	}

	s.raft, err = raft.NewRaft(s.config, s.fsm, s.logStore, s.stableStore, s.snapStore, s.raftTransport)
	if err != nil {
		return err
	}

	logging.Infof("Raft服务已启动 %s...", s.raftBind)

	return err
}

func (s *Server) bootstrapConfig(peers []PeerInfo) (*raft.Configuration, error) {
	hasState, err := raft.HasExistingState(s.logStore, s.stableStore, s.snapStore)
	if err != nil {
		return nil, err
	}

	if !hasState {
		raftConfig := &raft.Configuration{
			Servers: make([]raft.Server, len(peers)),
		}

		for i, p := range peers {
			raftConfig.Servers[i] = raft.Server{
				ID:      raft.ServerID(p.ID),
				Address: raft.ServerAddress(p.Address),
			}
		}

		return raftConfig, nil
	}

	return nil, nil
}

func (s *Server) raftStorePath() string {
	if s.raftLogStorePath == "" {
		return logStorePrefix + s.id
	}
	return s.raftLogStorePath
}

// FSM returns fsm.
func (s *Server) FSM() *FSM {
	return s.fsm
}

// Raft returns raft node.
func (s *Server) Raft() *raft.Raft {
	return s.raft
}

// IsLeader returns true if the current node is leader.
func (s *Server) IsLeader() bool {
	return s.raft.State() == raft.Leader
}

// ApplyCommand 将命令日志应用于状态机，以增加或删除成员。
func (s *Server) ApplyCommand(cmdType CommandType, data DaprHostMember) (bool, error) {
	if !s.IsLeader() {
		return false, errors.New("这不是leader节点")
	}

	cmdLog, err := makeRaftLogCommand(cmdType, data)
	if err != nil {
		return false, err
	}

	future := s.raft.Apply(cmdLog, commandTimeout)
	if err := future.Error(); err != nil {
		return false, err
	}

	resp := future.Response()
	return resp.(bool), nil
}

// Shutdown 优雅停止raft服务
func (s *Server) Shutdown() {
	if s.raft != nil {
		s.raftTransport.Close()
		future := s.raft.Shutdown()
		if err := future.Error(); err != nil {
			logging.Warnf("停止raft出错: %v", err)
		}
		if s.raftStore != nil {
			s.raftStore.Close()
		}
	}
}
