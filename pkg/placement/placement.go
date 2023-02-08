// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package placement

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/atomic"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dapr/kit/logger"

	dapr_credentials "github.com/dapr/dapr/pkg/credentials"
	"github.com/dapr/dapr/pkg/placement/raft"
	placementv1pb "github.com/dapr/dapr/pkg/proto/placement/v1"
)

var log = logger.NewLogger("dapr.placement")

// 服务端流
type placementGRPCStream placementv1pb.Placement_ReportDaprStatusServer

const (
	// membershipChangeChSize 是来自Dapr runtime的成员变更请求的通道大小。MembershipChangeWorker将处理actor主持的成员变更请求。
	membershipChangeChSize = 100

	// faultyHostDetectDuration是现有主机被标记为故障的最长时间。Dapr运行时每1秒发送一次心跳。每当安置服务器得到心跳，
	//它就更新FSM状态UpdateAt中的最后一次心跳时间。如果Now - UpdatedAt超过faultyHostDetectDuration，
	//membershipChangeWorker() 会尝试将有问题的Dapr runtime从 会员资格。
	// 当放置得到领导权时，faultyHostDetectionDuration将是faultyHostDetectInitialDuration。
	//这个持续时间将给予更多的时间让每个运行时找到安置节点的领导。一旦在获得领导权后发生第一次传播， membershipChangeWorker将
	// use faultyHostDetectDefaultDuration.
	faultyHostDetectInitialDuration = 6 * time.Second
	faultyHostDetectDefaultDuration = 3 * time.Second

	// faultyHostDetectInterval 是检查故障成员的间隔时间。
	faultyHostDetectInterval = 500 * time.Millisecond

	// disseminateTimerInterval 是传播最新一致的散列表的时间间隔。
	disseminateTimerInterval = 500 * time.Millisecond
	// disseminateTimeout 是actor节点变化后传播hash表的超时。当首先部署多个角色服务pod时，在开始时部署几个pod，
	//其余的pod将逐步部署。传播下一次时间被维护，以决定何时传播散列表。传播下一次时间在成员变化被应用到筏子状态或每个pod被部署时被更新。
	//如果我们增加 disseminateTimeout，将减少传播的频率，但会延迟表的传播。
	disseminateTimeout = 2 * time.Second
)

type hostMemberChange struct {
	cmdType raft.CommandType
	host    raft.DaprHostMember
}

// Service
//更新Dapr runtime，为有状态实体提供分布式哈希表。
type Service struct {
	// serverListener placement grpc服务的tcp listener
	serverListener net.Listener
	// grpcServerLock
	grpcServerLock *sync.Mutex
	// grpcServer placement grpc服务
	grpcServer *grpc.Server
	// streamConnPool  placement gRPC server and Dapr runtime之间建立的流连接
	streamConnPool []placementGRPCStream
	// streamConnPoolLock streamConnPool操作的锁
	streamConnPoolLock *sync.RWMutex

	// raftNode raft服务的节点
	raftNode *raft.Server

	// lastHeartBeat
	lastHeartBeat *sync.Map
	// membershipCh 用于管理dapr runtime成员更新的channel
	membershipCh chan hostMemberChange
	// disseminateLock 是hash表传播的锁。
	disseminateLock *sync.Mutex
	// disseminateNextTime hash表传播的时间
	disseminateNextTime atomic.Int64
	// memberUpdateCount 表示有多少dapr运行时间需要改变。一致的hash表。只有actor runtimes的心跳会增加这个。
	memberUpdateCount atomic.Uint32

	// faultyHostDetectDuration
	faultyHostDetectDuration *atomic.Int64 // 有故障的主机检测时间

	// hasLeadership 是否是leader
	hasLeadership atomic.Bool

	// streamConnGroup represents the number of stream connections.
	// This waits until all stream connections are drained when revoking leadership.
	//代表流连接的数量。
	//在撤销领导权时，这要等到所有的流连接被耗尽。
	streamConnGroup sync.WaitGroup

	// shutdownLock 停止锁
	shutdownLock *sync.Mutex
	// shutdownCh 优雅关闭的channel
	shutdownCh chan struct{}
}

// NewPlacementService 返回一个placement service
func NewPlacementService(raftNode *raft.Server) *Service {
	return &Service{
		disseminateLock:          &sync.Mutex{},
		streamConnPool:           []placementGRPCStream{},
		streamConnPoolLock:       &sync.RWMutex{},
		membershipCh:             make(chan hostMemberChange, membershipChangeChSize),
		faultyHostDetectDuration: atomic.NewInt64(int64(faultyHostDetectInitialDuration)),
		raftNode:                 raftNode,
		shutdownCh:               make(chan struct{}),
		grpcServerLock:           &sync.Mutex{},
		shutdownLock:             &sync.Mutex{},
		lastHeartBeat:            &sync.Map{},
	}
}

// Run 开启placement service gRPC server.
func (p *Service) Run(port string, certChain *dapr_credentials.CertChain) {
	var err error
	p.serverListener, err = net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	opts, err := dapr_credentials.GetServerOptions(certChain)
	if err != nil {
		log.Fatalf("error creating gRPC options: %s", err)
	}
	grpcServer := grpc.NewServer(opts...)
	var _ placementv1pb.PlacementServer = NewPlacementService(nil)
	placementv1pb.RegisterPlacementServer(grpcServer, p)
	p.grpcServerLock.Lock()
	p.grpcServer = grpcServer
	p.grpcServerLock.Unlock()

	if err := grpcServer.Serve(p.serverListener); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

// Shutdown 关闭所有服务端连接
func (p *Service) Shutdown() {
	p.shutdownLock.Lock()
	defer p.shutdownLock.Unlock()

	close(p.shutdownCh)

	// wait until hasLeadership is false by revokeLeadership()
	for p.hasLeadership.Load() {
		select {
		case <-time.After(5 * time.Second):
			goto TIMEOUT
		default:
			time.Sleep(500 * time.Millisecond)
		}
	}

TIMEOUT:
	p.grpcServerLock.Lock()
	if p.grpcServer != nil {
		p.grpcServer.Stop()
		p.grpcServer = nil
	}
	p.grpcServerLock.Unlock()
	p.serverListener.Close()
}

// ReportDaprStatus 获得节点的状态  (客户端的流,-->一直接收消息)
func (p *Service) ReportDaprStatus(stream placementv1pb.Placement_ReportDaprStatusServer) error {
	// 唯一客户端会请求的函数
	registeredMemberID := ""
	isActorRuntime := false // 是不是actor 实例

	p.streamConnGroup.Add(1)
	defer func() {
		p.streamConnGroup.Done()
		p.deleteStreamConn(stream)
	}()
	// 如果本节点是leader
	for p.hasLeadership.Load() {
		req, err := stream.Recv()
		//				Name:     p.runtimeHostName, // 10.10.16.72:50002  daprInternalGRPCPort
		//				Entities: p.actorTypes,      // []string
		//				Id:       p.appID,           // dp-61b1a6e4382df1ff8c3cdff1-workerapp
		//				Load:     1,                 // Not used yet
		switch err {
		case nil:
			if registeredMemberID == "" {
				registeredMemberID = req.Name // 实例地址
				p.addStreamConn(stream)
				// TODO: If each sidecar can report table version, then placement
				// 新注册的，返回已存在的所有actor数据。
				p.performTablesUpdate([]placementGRPCStream{stream}, p.raftNode.FSM().PlacementState())
				log.Debugf("Stream connection is established from %s", registeredMemberID)
			}

			// 确保传入的runtime是actor实例。
			isActorRuntime = len(req.Entities) > 0
			if !isActorRuntime {
				// 如果不是actor,忽略
				continue
			}

			// 记录心跳的时间戳。
			//这个时间戳将被用来检查由raft维护的成员状态是否有效。
			//如果该成员根据时间戳已经过期，该成员将被标记为有问题的节点并被删除。
			p.lastHeartBeat.Store(req.Name, time.Now().UnixNano())

			members := p.raftNode.FSM().State().Members()

			// Upsert incoming member only if it is an actor service (not actor client) and
			// the existing member info is unmatched with the incoming member info.
			// 只有当它是一个actor server（不是actor client），并且现有的成员信息与传入的成员信息不匹配时，才会将传入的成员上移。
			upsertRequired := true
			//				Name:     p.runtimeHostName, // 10.10.16.72:50002  daprInternalGRPCPort
			//				Entities: p.actorTypes,      // []string
			//				Id:       p.appID,           // dp-61b1a6e4382df1ff8c3cdff1-workerapp
			//				Load:     1,                 // Not used yet
			// appid 一致、ip:port 一致、actor 完全一致,则不用更新
			if m, ok := members[req.Name]; ok {
				if m.AppID == req.Id && m.Name == req.Name && cmp.Equal(m.Entities, req.Entities) {
					upsertRequired = false
				}
			}

			if upsertRequired {
				p.membershipCh <- hostMemberChange{
					cmdType: raft.MemberUpsert,
					host: raft.DaprHostMember{
						Name:      req.Name,
						AppID:     req.Id,
						Entities:  req.Entities,
						UpdatedAt: time.Now().UnixNano(),
					},
				}
			}

		default:
			if registeredMemberID == "" {
				log.Error("在添加成员之前，流被断开了")
				return nil
			}

			if err == io.EOF {
				log.Debugf("Stream链接已经被优雅关闭: %s", registeredMemberID)
				if isActorRuntime {
					p.membershipCh <- hostMemberChange{
						cmdType: raft.MemberRemove,
						host:    raft.DaprHostMember{Name: registeredMemberID},
					}
				}
			} else {
				// 没有对hash表的操作。相反，MembershipChangeWorker将检查主机的更新时间
				// 如果现在-更新时间>p.faultyHostDetectDuration，则删除主机。
				log.Debugf("流链接已经断开: %v", err)
			}

			return nil
		}
	}

	return status.Error(codes.FailedPrecondition, "只有leader才能处理请求")
}

// addStreamConn  dapr runtime <----> placement
func (p *Service) addStreamConn(conn placementGRPCStream) {
	p.streamConnPoolLock.Lock()
	p.streamConnPool = append(p.streamConnPool, conn)
	p.streamConnPoolLock.Unlock()
}

// OK
func (p *Service) deleteStreamConn(conn placementGRPCStream) {
	p.streamConnPoolLock.Lock()
	for i, c := range p.streamConnPool {
		if c == conn {
			p.streamConnPool = append(p.streamConnPool[:i], p.streamConnPool[i+1:]...)
			break
		}
	}
	p.streamConnPoolLock.Unlock()
}
