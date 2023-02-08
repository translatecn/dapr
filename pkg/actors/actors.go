// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package actors

import (
	"context"
	"encoding/json"
	"fmt"
	nethttp "net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/valyala/fasthttp"
	"go.uber.org/atomic"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dapr/components-contrib/state"
	"github.com/dapr/kit/logger"

	"github.com/dapr/dapr/pkg/actors/internal"
	"github.com/dapr/dapr/pkg/channel"
	configuration "github.com/dapr/dapr/pkg/config"
	dapr_credentials "github.com/dapr/dapr/pkg/credentials"
	diag "github.com/dapr/dapr/pkg/diagnostics"
	diag_utils "github.com/dapr/dapr/pkg/diagnostics/utils"
	"github.com/dapr/dapr/pkg/health"
	invokev1 "github.com/dapr/dapr/pkg/messaging/v1"
	"github.com/dapr/dapr/pkg/modes"
	commonv1pb "github.com/dapr/dapr/pkg/proto/common/v1"
	internalv1pb "github.com/dapr/dapr/pkg/proto/internals/v1"
	"github.com/dapr/dapr/pkg/retry"
)

const (
	DaprSeparator        = "||"
	metadataPartitionKey = "partitionKey"
)

var log = logger.NewLogger("dapr.runtime.actor")

var pattern = regexp.MustCompile(`^(R(?P<repetition>\d+)/)?P((?P<year>\d+)Y)?((?P<month>\d+)M)?((?P<week>\d+)W)?((?P<day>\d+)D)?(T((?P<hour>\d+)H)?((?P<minute>\d+)M)?((?P<second>\d+)S)?)?$`)

// Actors 允许调用虚拟actors以及actor的状态管理。
type Actors interface {
	Call(ctx context.Context, req *invokev1.InvokeMethodRequest) (*invokev1.InvokeMethodResponse, error)
	Init() error
	Stop()
	GetState(ctx context.Context, req *GetStateRequest) (*StateResponse, error)
	TransactionalStateOperation(ctx context.Context, req *TransactionalRequest) error
	GetReminder(ctx context.Context, req *GetReminderRequest) (*Reminder, error)
	CreateReminder(ctx context.Context, req *CreateReminderRequest) error
	DeleteReminder(ctx context.Context, req *DeleteReminderRequest) error
	CreateTimer(ctx context.Context, req *CreateTimerRequest) error
	DeleteTimer(ctx context.Context, req *DeleteTimerRequest) error
	IsActorHosted(ctx context.Context, req *ActorHostedRequest) bool
	GetActiveActorsCount(ctx context.Context) []ActiveActorsCount
}

type actorsRuntime struct {
	appChannel               channel.AppChannel
	store                    state.Store
	transactionalStore       state.TransactionalStore
	placement                *internal.ActorPlacement
	grpcConnectionFn         func(ctx context.Context, address, id string, namespace string, skipTLS, recreateIfExists, enableSSL bool, customOpts ...grpc.DialOption) (*grpc.ClientConn, error)
	config                   Config
	actorsTable              *sync.Map
	activeTimers             *sync.Map
	activeTimersLock         *sync.RWMutex
	activeReminders          *sync.Map
	remindersLock            *sync.RWMutex
	activeRemindersLock      *sync.RWMutex
	reminders                map[string][]actorReminderReference
	evaluationLock           *sync.RWMutex // 评估锁
	evaluationBusy           bool          // 评估 是否忙碌
	evaluationChan           chan bool     // 释放 评估不忙碌的信号
	appHealthy               *atomic.Bool
	certChain                *dapr_credentials.CertChain
	tracingSpec              configuration.TracingSpec
	reentrancyEnabled        bool
	actorTypeMetadataEnabled bool // actor 是否支持元数据
}

// ActiveActorsCount 包含actorType和每种类型所拥有的演员数量。
type ActiveActorsCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// ActorRemindersMetadata reminder 的消息
type ActorRemindersMetadata struct {
	PartitionCount int                `json:"partitionCount"`
	partitionsEtag map[uint32]*string `json:"-"`
}

type actorReminderReference struct {
	actorMetadataID           string
	actorRemindersPartitionID uint32
	reminder                  *Reminder
}

const (
	incompatibleStateStore = " 状态存储不支持事务，因为actor需要保存状态。 - please see https://docs.dapr.io/operations/components/setup-state-store/supported-state-stores/"
)

// NewActors 使用提供的actor配置创建actor实例
func NewActors(
	stateStore state.Store,
	appChannel channel.AppChannel,
	grpcConnectionFn func(ctx context.Context, address, id string, namespace string, skipTLS, recreateIfExists, enableSSL bool, customOpts ...grpc.DialOption) (*grpc.ClientConn, error),
	config Config,
	certChain *dapr_credentials.CertChain,
	tracingSpec configuration.TracingSpec,
	features []configuration.FeatureSpec) Actors {
	var transactionalStore state.TransactionalStore
	if stateStore != nil {
		features := stateStore.Features()
		if state.FeatureETag.IsPresent(features) && state.FeatureTransactional.IsPresent(features) {
			transactionalStore = stateStore.(state.TransactionalStore)
		}
	}

	return &actorsRuntime{
		appChannel:               appChannel,
		config:                   config,
		store:                    stateStore,
		transactionalStore:       transactionalStore, // 事务存储
		grpcConnectionFn:         grpcConnectionFn,
		actorsTable:              &sync.Map{},
		activeTimers:             &sync.Map{},
		activeTimersLock:         &sync.RWMutex{},
		activeReminders:          &sync.Map{},
		remindersLock:            &sync.RWMutex{},
		activeRemindersLock:      &sync.RWMutex{},
		reminders:                map[string][]actorReminderReference{},
		evaluationLock:           &sync.RWMutex{},
		evaluationBusy:           false,
		evaluationChan:           make(chan bool),
		appHealthy:               atomic.NewBool(true),
		certChain:                certChain,
		tracingSpec:              tracingSpec,
		reentrancyEnabled:        configuration.IsFeatureEnabled(features, configuration.ActorReentrancy) && config.Reentrancy.Enabled,
		actorTypeMetadataEnabled: configuration.IsFeatureEnabled(features, configuration.ActorTypeMetadata),
	}
}

func (a *actorsRuntime) Init() error {
	if len(a.config.PlacementAddresses) == 0 {
		return errors.New("actor：无法连接到placement服务：地址为空")
	}
	// 与 pkg/runtime/runtime.go:616 功能一致
	if len(a.config.HostedActorTypes) > 0 {
		if a.store == nil {
			log.Warn("actors: 状态存储必须存在，以初始化actor的运行时间")
		} else {
			features := a.store.Features()
			// 如果用户强制设置了actorStateStore 需要判断是不是支持这两个特性
			if !state.FeatureETag.IsPresent(features) || !state.FeatureTransactional.IsPresent(features) {
				return errors.New(incompatibleStateStore)
			}
		}
	}

	hostname := fmt.Sprintf("%s:%d", a.config.HostAddress, a.config.Port) //daprd 的内部grpc端口
	// 表更新之后调用的函数
	afterTableUpdateFn := func() {
		a.drainRebalancedActors()
		a.evaluateReminders()
	}
	appHealthFn := func() bool { return a.appHealthy.Load() } // 应用健康检查函数
	//初始化actor服务的 ActorPlacement结构
	a.placement = internal.NewActorPlacement(
		a.config.PlacementAddresses, a.certChain,
		a.config.AppID, hostname, a.config.HostedActorTypes,
		appHealthFn,
		afterTableUpdateFn)

	go a.placement.Start() // 1、注册自身 2、接收来自placement的信息， 包括所有的节点
	//a.placement.Start() // 1、注册自身 2、接收来自placement的信息， 包括所有的节点
	// todo  默认30s,1h
	a.startDeactivationTicker(a.config.ActorDeactivationScanInterval, a.config.ActorIdleTimeout)

	log.Infof("actor运行时已启动. actor 空闲超时: %s. actor 扫描间隔: %s",
		a.config.ActorIdleTimeout.String(), a.config.ActorDeactivationScanInterval.String())

	//在配置healthz端点选项时要小心。
	//如果app healthz返回不健康状态，Dapr将断开放置，将节点从一致的哈希环中移除。
	//例如，如果应用程序是忙碌的状态，健康状态将是不稳定的，这导致频繁的演员再平衡。这将影响整个服务。
	go a.startAppHealthCheck(
		health.WithFailureThreshold(4),           // 失败次数
		health.WithInterval(5*time.Second),       // 检查周期
		health.WithRequestTimeout(2*time.Second)) // 请求超时

	return nil
}

// 启动应用健康检查
func (a *actorsRuntime) startAppHealthCheck(opts ...health.Option) {
	if len(a.config.HostedActorTypes) == 0 {
		return
	}
	//http://127.0.0.1:3001/healthz
	healthAddress := fmt.Sprintf("%s/healthz", a.appChannel.GetBaseAddress())
	ch := health.StartEndpointHealthCheck(healthAddress, opts...)
	for {
		appHealthy := <-ch
		a.appHealthy.Store(appHealthy)
	}
}

// 拼接key
func constructCompositeKey(keys ...string) string {
	return strings.Join(keys, DaprSeparator)
}

// 拆分key
func decomposeCompositeKey(compositeKey string) []string {
	return strings.Split(compositeKey, DaprSeparator)
}

func (a *actorsRuntime) deactivateActor(actorType, actorID string) error {
	req := invokev1.NewInvokeMethodRequest(fmt.Sprintf("actors/%s/%s", actorType, actorID))
	req.WithHTTPExtension(nethttp.MethodDelete, "")
	req.WithRawData(nil, invokev1.JSONContentType)

	// TODO Propagate context
	ctx := context.Background()
	resp, err := a.appChannel.InvokeMethod(ctx, req)
	if err != nil {
		diag.DefaultMonitoring.ActorDeactivationFailed(actorType, "invoke")
		return err
	}

	if resp.Status().Code != nethttp.StatusOK {
		diag.DefaultMonitoring.ActorDeactivationFailed(actorType, fmt.Sprintf("status_code_%d", resp.Status().Code))
		_, body := resp.RawData()
		return errors.Errorf("error from actor service: %s", string(body))
	}

	actorKey := constructCompositeKey(actorType, actorID)
	a.actorsTable.Delete(actorKey)
	diag.DefaultMonitoring.ActorDeactivated(actorType)
	log.Debugf("deactivated actor type=%s, id=%s\n", actorType, actorID)

	return nil
}

// 根据key 获取actor信息
func (a *actorsRuntime) getActorTypeAndIDFromKey(key string) (string, string) {
	arr := decomposeCompositeKey(key)
	return arr[0], arr[1]
}

// 创建失活触发器   30s 60m
func (a *actorsRuntime) startDeactivationTicker(interval, actorIdleTimeout time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for t := range ticker.C {
			a.actorsTable.Range(func(key, value interface{}) bool {
				log.Info(key, value)
				actorInstance := value.(*actor)

				if actorInstance.isBusy() {
					return true
				}

				durationPassed := t.Sub(actorInstance.lastUsedTime)
				if durationPassed >= actorIdleTimeout {
					go func(actorKey string) {
						actorType, actorID := a.getActorTypeAndIDFromKey(actorKey)
						err := a.deactivateActor(actorType, actorID)
						if err != nil {
							log.Errorf("停止actor失败 %s: %s", actorKey, err)
						}
					}(key.(string))
				}

				return true
			})
		}
	}()
}

// Call ok
func (a *actorsRuntime) Call(ctx context.Context, req *invokev1.InvokeMethodRequest) (*invokev1.InvokeMethodResponse, error) {
	a.placement.WaitUntilPlacementTableIsReady()

	actor := req.Actor()
	targetActorAddress, appID := a.placement.LookupActor(actor.GetActorType(), actor.GetActorId())
	if targetActorAddress == "" {
		return nil, errors.Errorf("查找actor type %s; id %s 的地址出错", actor.GetActorType(), actor.GetActorId())
	}

	var resp *invokev1.InvokeMethodResponse
	var err error

	if a.isActorLocal(targetActorAddress, a.config.HostAddress, a.config.Port) {
		resp, err = a.callLocalActor(ctx, req) // 直接调用本身actor
	} else {
		// 重试次数、重试间隔、func、目标主机地址、目标主机应用ID、请求
		resp, err = a.callRemoteActorWithRetry(ctx, retry.DefaultLinearRetryCount,
			retry.DefaultLinearBackoffInterval, a.callRemoteActor, targetActorAddress, appID, req,
		)
	}

	if err != nil {
		return nil, err
	}
	return resp, nil
}

// callRemoteActorWithRetry
//将在指定的重试次数内调用一个远程行为体，并且只在瞬时失败的情况下重试。
func (a *actorsRuntime) callRemoteActorWithRetry(
	ctx context.Context,
	numRetries int,
	backoffInterval time.Duration,
	fn func(ctx context.Context, targetAddress, targetID string, req *invokev1.InvokeMethodRequest) (*invokev1.InvokeMethodResponse, error),
	targetAddress, targetID string, req *invokev1.InvokeMethodRequest) (*invokev1.InvokeMethodResponse, error) {
	for i := 0; i < numRetries; i++ {
		resp, err := fn(ctx, targetAddress, targetID, req)
		if err == nil {
			return resp, nil
		}
		time.Sleep(backoffInterval)

		code := status.Code(err)
		if code == codes.Unavailable || code == codes.Unauthenticated {
			_, err = a.grpcConnectionFn(context.TODO(), targetAddress, targetID, a.config.Namespace, false, true, false)
			if err != nil {
				return nil, err
			}
			continue
		}
		return resp, err
	}
	return nil, errors.Errorf("failed to invoke target %s after %v retries", targetAddress, numRetries)
}

// 获取或者创建一个actor
func (a *actorsRuntime) getOrCreateActor(actorType, actorID string) *actor {
	//构造组合键    actorType||actorID
	key := constructCompositeKey(actorType, actorID)

	// 这就避免了在每次调用actor时都调用newActor来分配多个actor。
	// 当首先存储actor key时，有机会调用newActor，但这是微不足道的。
	val, ok := a.actorsTable.Load(key)
	if !ok {
		val, _ = a.actorsTable.LoadOrStore(key, newActor(actorType, actorID, a.config.Reentrancy.MaxStackDepth))
	}

	return val.(*actor)
}

// 直接调用本身actor
func (a *actorsRuntime) callLocalActor(ctx context.Context, req *invokev1.InvokeMethodRequest) (*invokev1.InvokeMethodResponse, error) {
	actorTypeID := req.Actor()

	act := a.getOrCreateActor(actorTypeID.GetActorType(), actorTypeID.GetActorId())

	// 重入性来决定我们如何锁定。  可重入,则一定是线程安全的;类似于 递归,需要保存栈
	var reentrancyID *string
	if a.reentrancyEnabled {
		if headerValue, ok := req.Metadata()["Dapr-Reentrancy-Id"]; ok {
			reentrancyID = &headerValue.GetValues()[0]
		} else {
			reentrancyHeader := fasthttp.RequestHeader{}
			uuid := uuid.New().String()
			reentrancyHeader.Add("Dapr-Reentrancy-Id", uuid)
			req.AddHeaders(&reentrancyHeader)
			reentrancyID = &uuid
		}
	}

	err := act.lock(reentrancyID)
	if err != nil {
		return nil, status.Error(codes.ResourceExhausted, err.Error())
	}
	defer act.unlock()

	// 替换方法为actor方法
	req.Message().Method = fmt.Sprintf("actors/%s/%s/method/%s",
		actorTypeID.GetActorType(), actorTypeID.GetActorId(), req.Message().Method)
	// 原代码用PUT覆盖了方法。为什么？
	if req.Message().GetHttpExtension() == nil {
		req.WithHTTPExtension(nethttp.MethodPut, "")
	} else {
		req.Message().HttpExtension.Verb = commonv1pb.HTTPExtension_PUT
	}
	resp, err := a.appChannel.InvokeMethod(ctx, req)
	if err != nil {
		return nil, err
	}

	_, respData := resp.RawData()

	if resp.Status().Code != nethttp.StatusOK {
		return nil, errors.Errorf("error from actor service: %s", string(respData))
	}

	return resp, nil
}

// OK
func (a *actorsRuntime) callRemoteActor(
	ctx context.Context,
	targetAddress, targetID string,
	req *invokev1.InvokeMethodRequest) (*invokev1.InvokeMethodResponse, error) {
	conn, err := a.grpcConnectionFn(context.TODO(), targetAddress, targetID,
		a.config.Namespace, false, false, false)
	if err != nil {
		return nil, err
	}

	span := diag_utils.SpanFromContext(ctx)
	ctx = diag.SpanContextToGRPCMetadata(ctx, span.SpanContext())
	client := internalv1pb.NewServiceInvocationClient(conn)
	resp, err := client.CallActor(ctx, req.Proto()) // 远程sidecar的CallActor
	if err != nil {
		return nil, err
	}

	return invokev1.InternalInvokeResponse(resp)
}

// 判断目标actor地址是不是在本机,以及是不是actor本身
func (a *actorsRuntime) isActorLocal(targetActorAddress, hostAddress string, grpcPort int) bool {
	return strings.Contains(targetActorAddress, "localhost") ||
		strings.Contains(targetActorAddress, "127.0.0.1") ||
		targetActorAddress == fmt.Sprintf("%s:%v", hostAddress, grpcPort)
}

// GetState OK
func (a *actorsRuntime) GetState(ctx context.Context, req *GetStateRequest) (*StateResponse, error) {
	if a.store == nil {
		return nil, errors.New("actors: 状态存储不存在或配置不正确")
	}

	partitionKey := constructCompositeKey(a.config.AppID, req.ActorType, req.ActorID)
	metadata := map[string]string{metadataPartitionKey: partitionKey}

	key := a.constructActorStateKey(req.ActorType, req.ActorID, req.Key)
	resp, err := a.store.Get(&state.GetRequest{
		Key:      key,
		Metadata: metadata,
	})
	if err != nil {
		return nil, err
	}

	return &StateResponse{
		Data: resp.Data,
	}, nil
}

// TransactionalStateOperation 状态事务操作
func (a *actorsRuntime) TransactionalStateOperation(ctx context.Context, req *TransactionalRequest) error {
	if a.store == nil || a.transactionalStore == nil {
		return errors.New("actors：状态存储不存在或配置不正确")
	}
	var operations []state.TransactionalStateOperation
	partitionKey := constructCompositeKey(a.config.AppID, req.ActorType, req.ActorID)
	metadata := map[string]string{metadataPartitionKey: partitionKey} // 元数据分区key

	for _, o := range req.Operations {
		// 防止用户输入的Operation不是我们提供的
		switch o.Operation {
		case Upsert:
			var upsert TransactionalUpsert
			err := mapstructure.Decode(o.Request, &upsert)
			if err != nil {
				return err
			}
			key := a.constructActorStateKey(req.ActorType, req.ActorID, upsert.Key)
			operations = append(operations, state.TransactionalStateOperation{
				Request: state.SetRequest{
					Key:      key,
					Value:    upsert.Value,
					Metadata: metadata,
				},
				Operation: state.Upsert,
			})
		case Delete:
			var delete TransactionalDelete
			err := mapstructure.Decode(o.Request, &delete)
			if err != nil {
				return err
			}

			key := a.constructActorStateKey(req.ActorType, req.ActorID, delete.Key)
			operations = append(operations, state.TransactionalStateOperation{
				Request: state.DeleteRequest{
					Key:      key,
					Metadata: metadata,
				},
				Operation: state.Delete,
			})
		default:
			return errors.Errorf("操作类型不支持 %s ", o.Operation)
		}
	}

	err := a.transactionalStore.Multi(&state.TransactionalStateRequest{
		Operations: operations,
		Metadata:   metadata,
	})
	return err
}

func (a *actorsRuntime) IsActorHosted(ctx context.Context, req *ActorHostedRequest) bool {
	key := constructCompositeKey(req.ActorType, req.ActorID)
	// 需要actorsTable中同步其余数据
	_, exists := a.actorsTable.Load(key)
	return exists
}

// 构建actor状态存储的key
func (a *actorsRuntime) constructActorStateKey(actorType, actorID, key string) string {
	return constructCompositeKey(a.config.AppID, actorType, actorID, key)
}

// 重新平衡actor
func (a *actorsRuntime) drainRebalancedActors() {
	// 访问所有目前活跃的actor
	var wg sync.WaitGroup

	a.actorsTable.Range(func(key interface{}, value interface{}) bool {
		wg.Add(1)
		go func(key interface{}, value interface{}, wg *sync.WaitGroup) {
			defer wg.Done()
			// for each actor, deactivate if no longer hosted locally
			actorKey := key.(string)
			actorType, actorID := a.getActorTypeAndIDFromKey(actorKey)
			address, _ := a.placement.LookupActor(actorType, actorID)
			if address != "" && !a.isActorLocal(address, a.config.HostAddress, a.config.Port) {
				// actor has been moved to a different host, deactivate when calls are done cancel any reminders
				// each item in reminders contain a struct with some metadata + the actual reminder struct
				reminders := a.reminders[actorType]
				for _, r := range reminders {
					// r.reminder refers to the actual reminder struct that is saved in the db
					if r.reminder.ActorType == actorType && r.reminder.ActorID == actorID {
						reminderKey := constructCompositeKey(actorKey, r.reminder.Name)
						stopChan, exists := a.activeReminders.Load(reminderKey)
						if exists {
							close(stopChan.(chan bool))
							a.activeReminders.Delete(reminderKey)
						}
					}
				}

				actor := value.(*actor)
				if a.config.DrainRebalancedActors {
					// wait until actor isn't busy or timeout hits
					if actor.isBusy() {
						select {
						case <-time.After(a.config.DrainOngoingCallTimeout):
							break
						case <-actor.channel():
							// if a call comes in from the actor for state changes, that's still allowed
							break
						}
					}
				}

				// don't allow state changes
				a.actorsTable.Delete(key)

				diag.DefaultMonitoring.ActorRebalanced(actorType)

				for {
					// wait until actor is not busy, then deactivate
					if !actor.isBusy() {
						err := a.deactivateActor(actorType, actorID)
						if err != nil {
							log.Errorf("failed to deactivate actor %s: %s", actorKey, err)
						}
						break
					}
					time.Sleep(time.Millisecond * 500)
				}
			}
		}(key, value, &wg)
		return true
	})
}

// 保存actor type 的元数据
func (a *actorsRuntime) saveActorTypeMetadata(actorType string, actorMetadata *ActorMetadata) error {
	if !a.actorTypeMetadataEnabled {
		return nil
	}

	metadataKey := constructCompositeKey("actors", actorType, "metadata")
	return a.store.Set(&state.SetRequest{
		Key:   metadataKey,
		Value: actorMetadata,
		ETag:  actorMetadata.Etag,
	})
}

// 获取actor type的元数据
func (a *actorsRuntime) getActorTypeMetadata(actorType string, migrate bool) (*ActorMetadata, error) {
	if a.store == nil {
		return nil, errors.New("actors: 状态存储不存在或配置不正确")
	}

	if !a.actorTypeMetadataEnabled {
		return &ActorMetadata{
			ID: uuid.NewString(),
			RemindersMetadata: ActorRemindersMetadata{
				partitionsEtag: nil,
				PartitionCount: 0,
			},
			Etag: nil,
		}, nil
	}

	metadataKey := constructCompositeKey("actors", actorType, "metadata")
	resp, err := a.store.Get(&state.GetRequest{
		Key: metadataKey,
	})
	if err != nil || len(resp.Data) == 0 {
		// 元数据字段不存在或读取失败。我们回退到默认的 "零 "分区行为。
		actorMetadata := ActorMetadata{
			ID: uuid.NewString(),
			RemindersMetadata: ActorRemindersMetadata{
				partitionsEtag: nil,
				PartitionCount: 0,
			},
			Etag: nil,
		}

		// 保存元数据字段，以确保错误是由于没有找到记录。如果之前的错误是由于数据库造成的，那么这次写入将失败，原因是。
		//   1. 数据库仍然没有反应，或者
		//   2. etag不匹配，因为该项目已经存在。
		// 之所以需要这种写操作，也是因为我们要避免出现另一个挎包试图做同样事情的竞赛条件。
		etag := ""
		if resp != nil && resp.ETag != nil {
			etag = *resp.ETag
		}

		actorMetadata.Etag = &etag
		err = a.saveActorTypeMetadata(actorType, &actorMetadata)
		if err != nil {
			return nil, err
		}

		// 需要去读并获取etag
		resp, err = a.store.Get(&state.GetRequest{
			Key: metadataKey,
		})
		if err != nil {
			return nil, err
		}
	}

	var actorMetadata ActorMetadata
	err = json.Unmarshal(resp.Data, &actorMetadata)
	if err != nil {
		return nil, fmt.Errorf("不能解析actor type 元信息 %s (%s): %w", actorType, string(resp.Data), err)
	}
	actorMetadata.Etag = resp.ETag // 版本号
	if !migrate {
		return &actorMetadata, nil
	}

	return a.migrateRemindersForActorType(actorType, &actorMetadata)
}

// GetActiveActorsCount 获取活跃的actor数量
func (a *actorsRuntime) GetActiveActorsCount(ctx context.Context) []ActiveActorsCount {
	actorCountMap := map[string]int{}
	for _, actorType := range a.config.HostedActorTypes {
		actorCountMap[actorType] = 0
	}
	a.actorsTable.Range(func(key, value interface{}) bool {
		actorType, _ := a.getActorTypeAndIDFromKey(key.(string))
		actorCountMap[actorType]++
		return true
	})

	activeActorsCount := make([]ActiveActorsCount, 0, len(actorCountMap))
	for actorType, count := range actorCountMap {
		activeActorsCount = append(activeActorsCount, ActiveActorsCount{Type: actorType, Count: count})
	}

	return activeActorsCount
}

// Stop 关闭所有网络连接和actor runtime使用的资源。
func (a *actorsRuntime) Stop() {
	if a.placement != nil {
		a.placement.Stop()
	}
}

// ValidateHostEnvironment 验证在给定的一组参数和运行时的操作模式下，actor 可以被正确初始化。
func ValidateHostEnvironment(mTLSEnabled bool, mode modes.DaprMode, namespace string) error {
	switch mode {
	case modes.KubernetesMode:
		if mTLSEnabled && namespace == "" {
			return errors.New("actors 在Kubernetes模式下运行时，必须有一个配置好的命名空间")
		}
	}
	return nil
}
