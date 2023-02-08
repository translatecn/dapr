package runtime

import (
	"github.com/dapr/components-contrib/secretstores"
	components_v1alpha1 "github.com/dapr/dapr/pkg/apis/components/v1alpha1"
	"github.com/dapr/dapr/pkg/components"
	diag "github.com/dapr/dapr/pkg/diagnostics"
	"github.com/dapr/dapr/pkg/modes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (a *DaprRuntime) appendBuiltinSecretStore() {
	for _, comp := range a.builtinSecretStore() {
		a.pendingComponents <- comp
	}
}

func (a *DaprRuntime) builtinSecretStore() []components_v1alpha1.Component {
	// 预加载k8s的secret
	switch a.runtimeConfig.Mode {
	case modes.KubernetesMode:
		return []components_v1alpha1.Component{{
			ObjectMeta: metav1.ObjectMeta{
				Name: "kubernetes",
			},
			Spec: components_v1alpha1.ComponentSpec{
				Type:    "secretstores.kubernetes",
				Version: components.FirstStableVersion,
			},
		}}
	}
	return nil
}

func (a *DaprRuntime) initSecretStore(c components_v1alpha1.Component) error {
	// 会调用组件注册时，配置的工厂函数
	secretStore, err := a.secretStoresRegistry.Create(c.Spec.Type, c.Spec.Version) // secretstores.kubernetes  v1
	if err != nil {
		log.Warnf("未能创建秘密存储 %s/%s: %s", c.Spec.Type, c.Spec.Version, err)
		diag.DefaultMonitoring.ComponentInitFailed(c.Spec.Type, "creation")
		return err
	}

	err = secretStore.Init(secretstores.Metadata{
		Properties: a.convertMetadataItemsToProperties(c.Spec.Metadata),
	})
	if err != nil {
		log.Warnf("初始化秘密存储失败 %s/%s named %s: %s", c.Spec.Type, c.Spec.Version, c.ObjectMeta.Name, err)
		diag.DefaultMonitoring.ComponentInitFailed(c.Spec.Type, "init")
		return err
	}
	//       pkg/runtime/runtime.go:2110
	a.secretStores[c.ObjectMeta.Name] = secretStore
	// 记录指标
	diag.DefaultMonitoring.ComponentInitialized(c.Spec.Type)
	return nil
}

func (a *DaprRuntime) authSecretStoreOrDefault(comp components_v1alpha1.Component) string {
	if comp.SecretStore == "" {
		switch a.runtimeConfig.Mode {
		case modes.KubernetesMode:
			return "kubernetes"
		}
	}
	return comp.SecretStore
}

func (a *DaprRuntime) getSecretStore(storeName string) secretstores.SecretStore {
	if storeName == "" {
		return nil
	}
	return a.secretStores[storeName]
}

