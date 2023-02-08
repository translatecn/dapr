// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package internal

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"go.uber.org/atomic"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dapr/kit/logger"

	dapr_credentials "github.com/dapr/dapr/pkg/credentials"
	diag "github.com/dapr/dapr/pkg/diagnostics"
	"github.com/dapr/dapr/pkg/placement/hashing"
	v1pb "github.com/dapr/dapr/pkg/proto/placement/v1"
	"github.com/dapr/dapr/pkg/runtime/security"
)

var log = logger.NewLogger("dapr.runtime.actor.internal.placement")

const (
	lockOperation   = "lock"
	unlockOperation = "unlock"
	updateOperation = "update"

	placementReconnectInterval    = 500 * time.Millisecond
	statusReportHeartbeatInterval = 1 * time.Second // 1秒汇报一次状态

	grpcServiceConfig = `{"loadBalancingPolicy":"round_robin"}` // 轮询
)

// ActorPlacement 维护actor实例的成员 和一致性哈希当用于与placement service 进行服务发现时
type ActorPlacement struct {
	actorTypes []string
	appID      string
	// ip:port
	runtimeHostName string

	// serverAddr placement service 的地址数组【高可用】
	serverAddr []string
	// serverIndex 当前使用的是哪一个placement   ,数组下表
	serverIndex atomic.Int32

	// clientCert 与placement连接所用的证书链
	clientCert *dapr_credentials.CertChain

	// clientLock is the lock for client conn and stream.
	clientLock *sync.RWMutex
	// clientConn grpc 客户端连接
	clientConn *grpc.ClientConn
	// clientStream 是客户端流。
	clientStream v1pb.Placement_ReportDaprStatusClient
	// streamConnAlive 是客户端流是否存活
	streamConnAlive bool
	// streamConnectedCond is the condition variable for goroutines waiting for or announcing
	// that the stream between runtime and placement is connected.
	//是等待或宣布的goroutines的条件变量 ，用于等待或宣布runtime和placement 已经连接
	streamConnectedCond *sync.Cond

	// placementTables 一致性哈希表，用于寻找其他actor
	placementTables *hashing.ConsistentHashTables
	// placementTableLock 一致性哈希表 锁
	placementTableLock *sync.RWMutex

	// unblockSignal 用于解锁[placementTableLock]的channel
	unblockSignal chan struct{}
	// tableIsBlocked 表锁的状态
	tableIsBlocked *atomic.Bool
	// operationUpdateLock 是三阶段提交的锁。
	operationUpdateLock *sync.Mutex

	// appHealthFn 用户应用健康检查的回调函数
	appHealthFn func() bool
	// afterTableUpdateFn 是在表更新后进行的后处理的函数。 如排空演员和重设提醒等。
	afterTableUpdateFn func()

	// shutdown 是运行时被关闭时的标志。
	shutdown atomic.Bool
	// shutdownConnLoop 是等待组，等待所有的连接循环完成。
	shutdownConnLoop sync.WaitGroup
}

func addDNSResolverPrefix(addr []string) []string {
	resolvers := make([]string, 0, len(addr))
	for _, a := range addr {
		prefix := ""
		host, _, err := net.SplitHostPort(a)
		if err == nil && net.ParseIP(host) == nil {
			prefix = "dns:///"
		}
		resolvers = append(resolvers, a)
		resolvers = append(resolvers, prefix+a)
	}
	return resolvers
}

// NewActorPlacement 初始化actor服务的 ActorPlacement
func NewActorPlacement(
	serverAddr []string, clientCert *dapr_credentials.CertChain,
	appID, runtimeHostName string, actorTypes []string,
	appHealthFn func() bool,
	afterTableUpdateFn func()) *ActorPlacement {
	return &ActorPlacement{
		actorTypes:      actorTypes,
		appID:           appID,
		runtimeHostName: runtimeHostName,
		serverAddr:      addDNSResolverPrefix(serverAddr), // dapr-placement-server.dapr-system.svc.cluster.local:50005
		// dns:///dapr-placement-server.dapr-system.svc.cluster.local:50005
		clientCert: clientCert,

		clientLock:          &sync.RWMutex{},
		streamConnAlive:     false,
		streamConnectedCond: sync.NewCond(&sync.Mutex{}),

		placementTableLock: &sync.RWMutex{},
		placementTables:    &hashing.ConsistentHashTables{Entries: make(map[string]*hashing.Consistent)},

		operationUpdateLock: &sync.Mutex{},
		tableIsBlocked:      atomic.NewBool(false),
		appHealthFn:         appHealthFn,
		afterTableUpdateFn:  afterTableUpdateFn,
	}
}

// Start
//连接placement服务并注册，并定期发送心跳以报告当前的状态。
func (p *ActorPlacement) Start() {
	p.serverIndex.Store(0)
	p.shutdown.Store(false)

	established := false
	func() {
		p.clientLock.Lock()
		defer p.clientLock.Unlock()
		// 与placement建立流连接
		p.clientStream, p.clientConn = p.establishStreamConn()
		if p.clientStream == nil {
			return
		}
		established = true // 订阅placement
	}()
	if !established {
		return
	}

	func() {
		p.streamConnectedCond.L.Lock()
		defer p.streamConnectedCond.L.Unlock()

		p.streamConnAlive = true //  标记存活
		p.streamConnectedCond.Broadcast()
	}()

	// 建立接收通道，以检索placement table的更新
	p.shutdownConnLoop.Add(1)
	go func() {
		defer p.shutdownConnLoop.Done()
		for !p.shutdown.Load() {
			p.clientLock.RLock()
			clientStream := p.clientStream
			p.clientLock.RUnlock()
			resp, err := clientStream.Recv()
			if p.shutdown.Load() {
				break
			}

			// TODO: 我们以后可能需要处理特定的错误。
			if err != nil {
				p.closeStream()

				s, ok := status.FromError(err)
				// 如果当前的服务器不是领导者，那么它将尝试到下一个服务器。
				if ok && s.Code() == codes.FailedPrecondition {
					p.serverIndex.Store((p.serverIndex.Load() + 1) % int32(len(p.serverAddr)))
				} else {
					log.Debugf("disconnected from placement: %v", err)
				}

				newStream, newConn := p.establishStreamConn()
				if newStream != nil {
					p.clientLock.Lock()
					p.clientConn = newConn
					p.clientStream = newStream
					p.clientLock.Unlock()

					func() {
						p.streamConnectedCond.L.Lock()
						defer p.streamConnectedCond.L.Unlock()

						p.streamConnAlive = true
						p.streamConnectedCond.Broadcast()
					}()
				}

				continue
			}

			p.onPlacementOrder(resp)
		}
	}()

	// 向placement 发送当前的主机状态，用于注册，并通过placement维持成员的状态。
	p.shutdownConnLoop.Add(1)
	go func() {
		defer p.shutdownConnLoop.Done()
		for !p.shutdown.Load() {
			// 等待流连接建立
			func() {
				p.streamConnectedCond.L.Lock()
				defer p.streamConnectedCond.L.Unlock()

				for !p.streamConnAlive && !p.shutdown.Load() {
					p.streamConnectedCond.Wait()
				}
			}()

			if p.shutdown.Load() {
				break
			}

			if !p.appHealthFn() {
				// 应用不健康
				// app is unresponsive, close the stream and disconnect from the placement service.
				// Then Placement will remove this host from the member list.
				// 应用程序没有响应，关闭流并断开与放置服务的链接。
				// 然后Placement将从成员列表中删除该主机。
				log.Debug("disconnecting from placement service by the unhealthy app.")
				p.closeStream()
				continue
			}

			host := v1pb.Host{
				Name:     p.runtimeHostName, // 10.10.16.72:50002  daprInternalGRPCPort
				Entities: p.actorTypes,      // []string
				Id:       p.appID,           // dp-61b1a6e4382df1ff8c3cdff1-workerapp
				Load:     1,                 // Not used yet
				// Port是多余的，因为Name应该包含端口号
			}

			var err error
			// 加锁 以避免被调用与close end并发
			func() {
				p.clientLock.RLock()
				defer p.clientLock.RUnlock()

				// 当应用程序不健康、daprd与放置服务断开连接时，p.clientStream为nil。
				if p.clientStream != nil {
					err = p.clientStream.Send(&host)
				}
			}()

			if err != nil {
				diag.DefaultMonitoring.ActorStatusReportFailed("send", "status")
				log.Debugf("failed to report status to placement service : %v", err)
			}

			// 如果流连接不是活动的，则没有延迟。
			p.streamConnectedCond.L.Lock()
			streamConnAlive := p.streamConnAlive
			p.streamConnectedCond.L.Unlock()
			if streamConnAlive {
				diag.DefaultMonitoring.ActorStatusReported("send")
				time.Sleep(statusReportHeartbeatInterval)
			}
		}
	}()
}

// Stop shuts down server stream gracefully.
func (p *ActorPlacement) Stop() {
	// CAS to avoid stop more than once.
	if p.shutdown.CAS(false, true) {
		p.closeStream()
	}
	p.shutdownConnLoop.Wait()
}

func (p *ActorPlacement) closeStream() {
	func() {
		p.clientLock.Lock()
		defer p.clientLock.Unlock()

		if p.clientStream != nil {
			p.clientStream.CloseSend()
			p.clientStream = nil
		}

		if p.clientConn != nil {
			p.clientConn.Close()
			p.clientConn = nil
		}
	}()

	func() {
		p.streamConnectedCond.L.Lock()
		defer p.streamConnectedCond.L.Unlock()

		p.streamConnAlive = false
		// 唤醒阻塞在锁的
		p.streamConnectedCond.Broadcast()
	}()
}

// 与placement建立流连接,包括重新建立连接通过不同的index
func (p *ActorPlacement) establishStreamConn() (v1pb.Placement_ReportDaprStatusClient, *grpc.ClientConn) {
	for !p.shutdown.Load() {
		serverAddr := p.serverAddr[p.serverIndex.Load()] // 拿到placement众多地址中的一个    daprd 注入时指定的值

		// 停止与placement重新建立连接，知道 用户应用health Stop reconnecting to placement until app is healthy.
		if !p.appHealthFn() {
			//休眠0.5秒 再次检测应用程序健康状态
			time.Sleep(placementReconnectInterval)
			continue
		}

		log.Debugf("尝试与placement服务建立连接: %s", serverAddr)

		opts, err := dapr_credentials.GetClientOptions(p.clientCert, security.TLSServerName)
		if err != nil {
			log.Errorf("failed to establish TLS credentials for actor placement service: %s", err)
			return nil, nil
		}
		// grpc 是否监控
		if diag.DefaultGRPCMonitoring.IsEnabled() {
			opts = append(
				opts,
				grpc.WithUnaryInterceptor(diag.DefaultGRPCMonitoring.UnaryClientInterceptor()))
		}

		if len(p.serverAddr) == 1 && strings.HasPrefix(p.serverAddr[0], "dns:///") {
			// 在k8s环境中, dapr-placement 无头服务可以解析出多个ip
			// 使用随机负载均衡器，
			opts = append(opts, grpc.WithDefaultServiceConfig(grpcServiceConfig))
		}

		conn, err := grpc.Dial(serverAddr, opts...)
	NEXT_SERVER: // 主要是为了一定要与一个placement 建立连接
		if err != nil {
			log.Debugf("链接 to placement service出错: %v", err)
			if conn != nil {
				conn.Close()
			}
			p.serverIndex.Store((p.serverIndex.Load() + 1) % int32(len(p.serverAddr))) // 下一个IP的索引
			time.Sleep(placementReconnectInterval)
			continue
		}

		client := v1pb.NewPlacementClient(conn)                      // 单纯封装
		stream, err := client.ReportDaprStatus(context.Background()) // 汇报dapr状态, 流函数
		if err != nil {
			goto NEXT_SERVER
		}

		log.Debugf("建立了与 placement 的链接 at %s", conn.Target())
		return stream, conn
	}

	return nil, nil
}

// 处理placement的消息[actor 节点的变更]
func (p *ActorPlacement) onPlacementOrder(in *v1pb.PlacementOrder) {
	log.Debugf("接收到placement消息: %s", in.Operation)
	diag.DefaultMonitoring.ActorPlacementTableOperationReceived(in.Operation)

	// 只允许同时有一个update操作
	p.operationUpdateLock.Lock()
	defer p.operationUpdateLock.Unlock()

	//1、lock
	//2、update
	//3、unlock
	switch in.Operation {
	case lockOperation: // 锁定
		p.blockPlacements()

		go func() {
			// TODO: 使用无锁表更新.
			// current implementation is distributed two-phase locking algorithm.
			// If placement experiences intermittently outage during updateplacement,
			// user application will face 5 second blocking even if it can avoid deadlock.
			// It can impact the entire system.
			// 目前实现的是分布式两相锁算法。如果放置在更新放置过程中出现间歇性中断，
			// 即使可以避免死锁，用户应用程序也会面临5秒的阻塞。它会影响整个系统。
			time.Sleep(time.Second * 5)
			p.unblockPlacements()
		}()

	case unlockOperation: // 解锁
		p.unblockPlacements()

	case updateOperation: // 更新
		p.updatePlacements(in.Tables)
	}
}

func (p *ActorPlacement) blockPlacements() {
	p.unblockSignal = make(chan struct{})
	p.tableIsBlocked.Store(true)
}

func (p *ActorPlacement) unblockPlacements() {
	// 一定要已经加过锁
	if p.tableIsBlocked.CAS(true, false) {
		close(p.unblockSignal)
	}
}

func (p *ActorPlacement) updatePlacements(in *v1pb.PlacementTables) {
	updated := false
	func() {
		p.placementTableLock.Lock()
		defer p.placementTableLock.Unlock()

		if in.Version == p.placementTables.Version {
			return
		}
		/// 这里是全量覆盖诶
		tables := &hashing.ConsistentHashTables{Entries: make(map[string]*hashing.Consistent)}
		for k, v := range in.Entries {
			loadMap := map[string]*hashing.Host{}
			for lk, lv := range v.LoadMap {
				loadMap[lk] = hashing.NewHost(lv.Name, lv.Id, lv.Load, lv.Port)
			}
			tables.Entries[k] = hashing.NewFromExisting(v.Hosts, v.SortedSet, loadMap)
		}

		p.placementTables = tables
		p.placementTables.Version = in.Version
		updated = true
	}()

	if !updated {
		return
	}

	// 可以在内部调用LookupActor，所以不应该在placement table lock锁定的情况下这样做。
	p.afterTableUpdateFn()

	log.Infof("placement tables updated, version: %s", in.GetVersion())
}

// WaitUntilPlacementTableIsReady 等待，直到placement表被解锁。
func (p *ActorPlacement) WaitUntilPlacementTableIsReady() {
	// 如果锁定了，等待解锁信号
	if p.tableIsBlocked.Load() {
		<-p.unblockSignal
	}
}

// LookupActor 使用一致的哈希表解析到actor服务实例地址。
func (p *ActorPlacement) LookupActor(actorType, actorID string) (string, string) {
	p.placementTableLock.RLock()
	defer p.placementTableLock.RUnlock()

	if p.placementTables == nil {
		return "", ""
	}

	t := p.placementTables.Entries[actorType]
	if t == nil {
		return "", ""
	}
	//同一个actorType有不同的实例，每个实例都有自己的actorID,   ---> 获取到对应的实例【拿到主机名，应用ID】
	host, err := t.GetHost(actorID)
	if err != nil || host == nil {
		return "", ""
	}
	return host.Name, host.AppID
}


