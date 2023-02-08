// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package placement

import (
	"sync"
	"time"

	"google.golang.org/grpc/peer"

	"github.com/dapr/dapr/pkg/placement/monitoring"
	"github.com/dapr/dapr/pkg/placement/raft"
	v1pb "github.com/dapr/dapr/pkg/proto/placement/v1"

	"github.com/dapr/kit/retry"
)

const (
	// raftApplyCommandMaxConcurrency is the max concurrency to apply command log to raft.
	raftApplyCommandMaxConcurrency = 10
	barrierWriteTimeout            = 2 * time.Minute
)

// MonitorLeadership 	// 监听leader变化,借鉴的consul的代码
//是用来监督我们是否获得或失去我们的角色 作为Raft集群的领导者。领导者有一些工作要做
//因此，我们必须对变化作出反应
//
// reference: https://github.com/hashicorp/consul/blob/master/agent/consul/leader.go
func (p *Service) MonitorLeadership() {
	var weAreLeaderCh chan struct{}
	var leaderLoop sync.WaitGroup

	leaderCh := p.raftNode.Raft().LeaderCh()

	for {
		select {
		case isLeader := <-leaderCh:
			if isLeader {
				// 可能在本身就是leader的情况下
				if weAreLeaderCh != nil {
					log.Error("试图在运行过程中启动leader loop")
					continue
				}

				weAreLeaderCh = make(chan struct{})
				leaderLoop.Add(1)
				go func(ch chan struct{}) {
					defer leaderLoop.Done()
					p.leaderLoop(ch)
				}(weAreLeaderCh)
				log.Info("获得的集群领导权")
			} else {
				if weAreLeaderCh == nil {
					log.Error("试图在不运行的情况下停止leader loop")
					continue
				}

				log.Info("停止 leader loop")
				close(weAreLeaderCh)
				leaderLoop.Wait() // 确保上边的go routinue 正常结束了
				weAreLeaderCh = nil
				log.Info("丢失集群领导权")
			}

		case <-p.shutdownCh:
			return
		}
	}
}

func (p *Service) leaderLoop(stopCh chan struct{}) {
	// 这个循环是为了确保FSM通过应用屏障来反映所有排队的写入，并在成为领导之前完成领导的建立。
	for !p.hasLeadership.Load() {
		// 如果不是 leader
		// for earlier stop
		select {
		case <-stopCh:
			return
		case <-p.shutdownCh:
			return
		default:
		}
		//是用来发布一个命令，该命令会阻断直到所有前面的操作都被应用到FSM上。
		//它可以用来确保FSM反映所有排队的写操作。可以提供一个可选的超时来限制我们等待命令启动的时间。
		//这必须在领导者上运行，否则会失败。
		barrier := p.raftNode.Raft().Barrier(barrierWriteTimeout)
		if err := barrier.Error(); err != nil {
			log.Error("没能等到屏障", "error", err)
			continue
		}

		if !p.hasLeadership.Load() {
			p.establishLeadership()
			log.Info("获得 leader.")
			// 撤销leader 的过程必须在leaderLoop()结束前完成。
			defer p.revokeLeadership()
		}
	}

	p.membershipChangeWorker(stopCh)
}

// 确立leader角色
func (p *Service) establishLeadership() {
	// Give more time to let each runtime to find the leader and connect to the leader.
	// 给予更多的时间，让每个runtime找到leader并与leader连接。
	p.faultyHostDetectDuration.Store(int64(faultyHostDetectInitialDuration))

	p.membershipCh = make(chan hostMemberChange, membershipChangeChSize)
	p.hasLeadership.Store(true)
}

//  撤销领导者角色
func (p *Service) revokeLeadership() {
	p.hasLeadership.Store(false)

	log.Info("等到所有的连接都被耗尽")
	p.streamConnGroup.Wait()

	p.cleanupHeartbeats()
}

// 清理心跳
func (p *Service) cleanupHeartbeats() {
	p.lastHeartBeat.Range(func(key, value interface{}) bool {
		p.lastHeartBeat.Delete(key)
		return true
	})
}

// membershipChangeWorker  更新成员状态的哈希表
func (p *Service) membershipChangeWorker(stopCh chan struct{}) {
	faultyHostDetectTimer := time.NewTicker(faultyHostDetectInterval) // 节点删除检测周期
	disseminateTimer := time.NewTicker(disseminateTimerInterval)      // 定时广播最新的 hash表

	p.memberUpdateCount.Store(0)

	go p.processRaftStateCommand(stopCh)

	for {
		select {
		case <-stopCh:
			faultyHostDetectTimer.Stop()
			disseminateTimer.Stop()
			return

		case <-p.shutdownCh:
			faultyHostDetectTimer.Stop()
			disseminateTimer.Stop()
			return

		case t := <-disseminateTimer.C: // 500ms检查一次
			// 当不是leader时，不会广播
			if !p.hasLeadership.Load() {
				continue
			}

			// 检查actor runtime 的成员是否有变化; 	disseminateNextTime 是加2s;且当前channel中没有数据
			if p.disseminateNextTime.Load() <= t.UnixNano() && len(p.membershipCh) == 0 {
				if cnt := p.memberUpdateCount.Load(); cnt > 0 {
					log.Debugf("Add raft.TableDisseminate to membershipCh. memberUpdateCount count: %d", cnt)
					p.membershipCh <- hostMemberChange{cmdType: raft.TableDisseminate}
				}
			}

		case t := <-faultyHostDetectTimer.C:
			// 用于将一些汇报超时的节点数据剔除
			if !p.hasLeadership.Load() {
				continue
			}

			// 每个dapr运行时每隔500ms发送一次心跳，安置将更新UpdateAt时间戳。
			//如果UpdateAt已经过期，我们可以将该主机标记为有问题的节点。
			//这个有问题的主机将在下一个传播期从成员中删除。
			if len(p.membershipCh) == 0 {
				m := p.raftNode.FSM().State().Members()
				for _, v := range m {
					if !p.hasLeadership.Load() {
						break
					}

					// 当领导者被改变，当前的放置节点成为新的领导者时，没有来自每个运行时的流连接，
					//因此lastHeartBeat没有来自每个运行时的心跳时间戳记录。最终，所有的运行时都会找到领导者并连接到放置服务器的领导者。
					//在所有运行时连接到放置服务器的领导者之前，它将记录当前时间作为心跳时间戳。
					heartbeat, _ := p.lastHeartBeat.LoadOrStore(v.Name, time.Now().UnixNano())

					elapsed := t.UnixNano() - heartbeat.(int64)
					if elapsed < p.faultyHostDetectDuration.Load() {
						continue
					}
					log.Debugf("尝试删除过时的主机: %s, elapsed: %d ns", v.Name, elapsed)

					p.membershipCh <- hostMemberChange{
						cmdType: raft.MemberRemove,
						host:    raft.DaprHostMember{Name: v.Name},
					}
				}
			}
		}
	}
}

// processRaftStateCommand  将成员变更事件，同步到raft，并广播到所有dapr runtime
func (p *Service) processRaftStateCommand(stopCh chan struct{}) {
	// logApplyConcurrency 限制raft apply command 的并发
	logApplyConcurrency := make(chan struct{}, raftApplyCommandMaxConcurrency)

	for {
		select {
		case <-stopCh:
			return

		case <-p.shutdownCh:
			return
		//	pkg/placement/placement.go:237
		case op := <-p.membershipCh:
			switch op.cmdType {
			case raft.MemberUpsert, raft.MemberRemove:
				// MemberUpsert 每当Dapr runtime每隔1秒发送心跳时，就会更新dapr runtime主机的状态。
				// MemberRemove will be queued by faultHostDetectTimer.
				// 及时apply command 失败,所有的命令都会被重试知道状态一致
				logApplyConcurrency <- struct{}{}
				go func() {
					// 我们锁定传播，以确保更新可以在表被传播之前完成。  互斥锁
					p.disseminateLock.Lock()
					defer p.disseminateLock.Unlock()
					// raft 同步记录
					updated, raftErr := p.raftNode.ApplyCommand(op.cmdType, op.host)
					if raftErr != nil {
						log.Errorf("apply command: %v", raftErr)
					} else {
						if op.cmdType == raft.MemberRemove {
							p.lastHeartBeat.Delete(op.host.Name)
						}

						if updated {
							p.memberUpdateCount.Inc()
							// disseminateNextTime将在应用完成后被更新，这样它将不断地移动传播表的时间，这将减少不必要的表传播。
							p.disseminateNextTime.Store(time.Now().Add(disseminateTimeout).UnixNano())
						}
					}
					<-logApplyConcurrency
				}()

			case raft.TableDisseminate:
				// TableDisseminate将由 disseminateTimer触发。
				// 这就把最新的一致的hash表传播给Dapr runtime。
				p.performTableDissemination()
			}
		}
	}
}

//OK
func (p *Service) performTableDissemination() {
	p.streamConnPoolLock.RLock()
	nStreamConnPool := len(p.streamConnPool) // 当前有多少个 dapr runtime 客户端
	p.streamConnPoolLock.RUnlock()
	nTargetConns := len(p.raftNode.FSM().State().Members()) //有多少个actor实例
	monitoring.RecordRuntimesCount(nStreamConnPool)
	monitoring.RecordActorRuntimesCount(nTargetConns)
	// 如果没有成员更新，则不传播。
	if cnt := p.memberUpdateCount.Load(); cnt > 0 {
		p.disseminateLock.Lock()
		defer p.disseminateLock.Unlock()
		state := p.raftNode.FSM().PlacementState()
		log.Infof(
			"开始广播数据. 成员更新次数: %d, streams: %d, targets: %d, table generation: %s",
			cnt, nStreamConnPool, nTargetConns, state.Version)
		p.streamConnPoolLock.RLock()
		streamConnPool := make([]placementGRPCStream, len(p.streamConnPool))
		copy(streamConnPool, p.streamConnPool)
		p.streamConnPoolLock.RUnlock()
		p.performTablesUpdate(streamConnPool, state) // 将当前的state信息,传输到每一个stream客户端
		log.Infof(
			"广播数据完成. 成员更新次数: %d, streams: %d, targets: %d, table generation: %s",
			cnt, nStreamConnPool, nTargetConns, state.Version)
		p.memberUpdateCount.Store(0)

		// set faultyHostDetectDuration to the default duration.
		p.faultyHostDetectDuration.Store(int64(faultyHostDetectDefaultDuration))
	}
}

// performTablesUpdate updates the connected dapr runtimes using a 3 stage commit.
// It first locks so no further dapr can be taken it. Once placement table is locked
// in runtime, it proceeds to update new table to Dapr runtimes and then unlock
// once all runtimes have been updated.
//使用3个阶段的提交更新所连接的dapr运行时。它首先锁定，所以不能再有任何dapr被占用。
//一旦安置表在运行时被锁定，它将继续更新新表到Dapr运行时，然后解锁 一旦所有的运行时都被更新了，再解锁。
func (p *Service) performTablesUpdate(hosts []placementGRPCStream, newTable *v1pb.PlacementTables) {
	// TODO: error from disseminationOperation needs to be handle properly.
	// Otherwise, each Dapr runtime will have inconsistent hashing table.
	p.disseminateOperation(hosts, "lock", nil)
	p.disseminateOperation(hosts, "update", newTable)
	p.disseminateOperation(hosts, "unlock", nil)
	//	现有逻辑: 某个actor Type change ,eg A，会将所有的actor runtime 【client】所保存的actor信息全部替换
	// 	then：
	//		向所有hosts发送lock,此时如果client 调用某一个actor A; 需要等待 WaitUntilPlacementTableIsReady 执行完毕;
	//  那么这就会导致集群中所有的client调用全部阻塞
	//	 如果过程现有的样子，那么只会阻塞某一个调用actor A 的 client

}

func (p *Service) disseminateOperation(targets []placementGRPCStream, operation string, tables *v1pb.PlacementTables) error {
	o := &v1pb.PlacementOrder{
		Operation: operation,
		Tables:    tables,
	}

	var err error
	for _, s := range targets {
		config := retry.DefaultConfig()
		config.MaxRetries = 3
		backoff := config.NewBackOff()

		retry.NotifyRecover(
			func() error {
				err = s.Send(o)

				if err != nil {
					remoteAddr := "n/a"
					if peer, ok := peer.FromContext(s.Context()); ok {
						remoteAddr = peer.Addr.String()
					}

					log.Errorf("runtime host 更新(%q) on %q operation: %s", remoteAddr, operation, err)
					return err
				}
				return nil
			},
			backoff,
			func(err error, d time.Duration) { log.Debugf("试图再次传播数据 after error: %v", err) },
			func() { log.Debug("传播成功.") })
	}

	return err
}

//-id dapr-placement-0 -port 30001 -initial-cluster dapr-placement-0=127.0.0.1:8201,dapr-placement-1=127.0.0.1:8202,dapr-placement-2=127.0.0.1:8203
