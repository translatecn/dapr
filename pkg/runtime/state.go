package runtime

import (
	"github.com/dapr/components-contrib/state"
	components_v1alpha1 "github.com/dapr/dapr/pkg/apis/components/v1alpha1"
	state_loader "github.com/dapr/dapr/pkg/components/state"
	"github.com/dapr/dapr/pkg/config"
	diag "github.com/dapr/dapr/pkg/diagnostics"
	"github.com/dapr/dapr/pkg/encryption"
)

// 请参考状态存储api的决定  https://github.com/dapr/dapr/blob/master/docs/decision_records/api/API-008-multi-state-store-api-design.md
func (a *DaprRuntime) initState(s components_v1alpha1.Component) error {
	// 调用 github.com/dapr/components-contrib/state.Store 里的构造函数
	//todo  keyPrefix
	store, err := a.stateStoreRegistry.Create(s.Spec.Type, s.Spec.Version) // 也没干什么事，就是封装
	if err != nil {
		log.Warnf("创建状态存储的错误 %s (%s/%s): %s", s.ObjectMeta.Name, s.Spec.Type, s.Spec.Version, err)
		diag.DefaultMonitoring.ComponentInitFailed(s.Spec.Type, "creation")
		return err
	}
	if store != nil {
		secretStoreName := a.authSecretStoreOrDefault(s) // 获取存储secret的方式，   没设置,且运行在k8s 则为kubernets
		// 判断 State.Encryption 在全局配置中有没有开启
		if config.IsFeatureEnabled(a.globalConfig.Spec.Features, config.StateEncryption) {
			secretStore := a.getSecretStore(secretStoreName)
			encKeys, encErr := encryption.ComponentEncryptionKey(s, secretStore)
			if encErr != nil {
				log.Errorf("错误初始化状态存储加密 %s (%s/%s): %s", s.ObjectMeta.Name, s.Spec.Type, s.Spec.Version, encErr)
				diag.DefaultMonitoring.ComponentInitFailed(s.Spec.Type, "creation")
				return encErr
			}

			if encKeys.Primary.Key != "" {
				ok := encryption.AddEncryptedStateStore(s.ObjectMeta.Name, encKeys)
				if ok {
					log.Infof("启用状态存储的自动加密功能 %s", s.ObjectMeta.Name)
				}
			}
		}
		//将用户填写的spec.metadata 中包含{uuid}的键值对 转换成属性
		props := a.convertMetadataItemsToProperties(s.Spec.Metadata)
		err = store.Init(state.Metadata{
			Properties: props,
		}) // 建立连接
		if err != nil {
			diag.DefaultMonitoring.ComponentInitFailed(s.Spec.Type, "init")
			log.Warnf("初始化状态存储的错误 %s (%s/%s): %s", s.ObjectMeta.Name, s.Spec.Type, s.Spec.Version, err)
			return err
		}

		a.stateStores[s.ObjectMeta.Name] = store
		err = state_loader.SaveStateConfiguration(s.ObjectMeta.Name, props)
		if err != nil {
			diag.DefaultMonitoring.ComponentInitFailed(s.Spec.Type, "init")
			log.Warnf("错误保存状态keyprefix: %s", err.Error())
			return err
		}

		// 如果 "actorStateStore "在yaml中为真，则设置指定的角色存储。
		actorStoreSpecified := props[actorStateStore] // actor状态存储，只取第一个标记了actorStateStore的
		if actorStoreSpecified == "true" {
			if a.actorStateStoreCount++; a.actorStateStoreCount == 1 {
				a.actorStateStoreName = s.ObjectMeta.Name
			}
		}
		diag.DefaultMonitoring.ComponentInitialized(s.Spec.Type)
	}
	// 配置了actor类型，且设置了actorStateStore
	if a.hostingActors() && (a.actorStateStoreName == "" || a.actorStateStoreCount != 1) {
		log.Warnf("在配置中没有指定行为体状态存储或多个行为体状态存储，则指定的行为体存储为: %d", a.actorStateStoreCount)
	}

	return nil
}

