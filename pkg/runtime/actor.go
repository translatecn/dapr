package runtime

import "github.com/dapr/dapr/pkg/actors"

func (a *DaprRuntime) initActors() error {
	// 没什么实际功能
	err := actors.ValidateHostEnvironment(a.runtimeConfig.mtlsEnabled, a.runtimeConfig.Mode, a.namespace) //true   kubernetes mesoid
	if err != nil {
		return err
	}
	actorConfig := actors.NewConfig(a.hostAddress, a.runtimeConfig.ID, a.runtimeConfig.PlacementAddresses, a.appConfig.Entities,
		a.runtimeConfig.InternalGRPCPort, a.appConfig.ActorScanInterval, a.appConfig.ActorIdleTimeout, a.appConfig.DrainOngoingCallTimeout,
		a.appConfig.DrainRebalancedActors, a.namespace, a.appConfig.Reentrancy, a.appConfig.RemindersStoragePartitions)
	// 判断有没有设置的actor的stateStore  有没有ETAG 和FeatureTransactional 两个属性
	act := actors.NewActors(a.stateStores[a.actorStateStoreName], a.appChannel, a.grpc.GetGRPCConnection, actorConfig, a.runtimeConfig.CertChain, a.globalConfig.Spec.TracingSpec, a.globalConfig.Spec.Features)
	err = act.Init()// 检测stateStore 不能为空； 与placement通信； app健康检查
	a.actor = act
	return err
}

func (a *DaprRuntime) hostingActors() bool {
	return len(a.appConfig.Entities) > 0
}

func (a *DaprRuntime) stopActor() {
	if a.actor != nil {
		log.Info("Shutting down actor")
		a.actor.Stop()
	}
}

