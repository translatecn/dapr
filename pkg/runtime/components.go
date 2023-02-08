package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/cenkalti/backoff/v4"
	"github.com/dapr/components-contrib/bindings"
	"github.com/dapr/components-contrib/configuration"
	"github.com/dapr/components-contrib/pubsub"
	"github.com/dapr/components-contrib/secretstores"
	"github.com/dapr/components-contrib/state"
	daprd_debug "github.com/dapr/dapr/code_debug/daprd"
	"github.com/dapr/dapr/pkg/actors"
	components_v1alpha1 "github.com/dapr/dapr/pkg/apis/components/v1alpha1"
	"github.com/dapr/dapr/pkg/components"
	diag "github.com/dapr/dapr/pkg/diagnostics"
	"github.com/dapr/dapr/pkg/messaging"
	"github.com/dapr/dapr/pkg/modes"
	operatorv1pb "github.com/dapr/dapr/pkg/proto/operator/v1"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	"io"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"path"
	"reflect"
	"strings"
	"time"
)

type ComponentCategory string

const (
	bindingsComponent               ComponentCategory = "bindings"
	pubsubComponent                 ComponentCategory = "pubsub"
	secretStoreComponent            ComponentCategory = "secretstores"
	stateComponent                  ComponentCategory = "state"
	middlewareComponent             ComponentCategory = "middleware"
	configurationComponent          ComponentCategory = "configuration"
	defaultComponentInitTimeout                       = time.Second * 5
	defaultGracefulShutdownDuration                   = time.Second * 5
	kubernetesSecretStore                             = "kubernetes"
)

var componentCategoriesNeedProcess = []ComponentCategory{
	bindingsComponent,
	pubsubComponent,
	secretStoreComponent,
	stateComponent,
	middlewareComponent,
	configurationComponent,
}

type ComponentsCallback func(components ComponentRegistry) error

type ComponentRegistry struct {
	Actors          actors.Actors
	DirectMessaging messaging.DirectMessaging
	StateStores     map[string]state.Store
	InputBindings   map[string]bindings.InputBinding
	OutputBindings  map[string]bindings.OutputBinding
	SecretStores    map[string]secretstores.SecretStore
	PubSubs         map[string]pubsub.PubSub
}

type componentPreprocessRes struct {
	unreadyDependency string
}

// 判断组件有没有经过认证
//apiVersion: dapr.io/v1alpha1
//kind: Component
//metadata:
//  name: statestore
//  namespace: liushuo
//  uid: 91d1b0a0-684f-4f1e-814d-8240bc7f24f3
//spec:
//  metadata:
//    - name: redisHost
//      value: 'redis:6379'
//    - name: redisPassword
//      value: ''
//  type: state.redis
//  version: v1
//auth:
//  secretStore: kubernetes

func (a *DaprRuntime) isComponentAuthorized(component components_v1alpha1.Component) bool {
	// mesoid
	if a.namespace == "" || (a.namespace != "" && component.ObjectMeta.Namespace == a.namespace) {
		if len(component.Scopes) == 0 {
			return true
		}
		// 定义了作用域，确保这个运行时ID被授权。
		// scopes are defined, make sure this runtime ID is authorized
		for _, s := range component.Scopes {
			if s == a.runtimeConfig.ID {
				return true
			}
		}
	}

	return false
}

//  只有一个入口
func (a *DaprRuntime) loadComponents(opts *runtimeOpts) error {
	var loader components.ComponentLoader

	switch a.runtimeConfig.Mode {
	case modes.KubernetesMode:
		loader = components.NewKubernetesComponents(a.runtimeConfig.Kubernetes, a.namespace, a.operatorClient)
	case modes.StandaloneMode:
		userHome, err := daprd_debug.Home()
		if err != nil {
			log.Fatal(err)
		}
		a.runtimeConfig.Standalone.ComponentsPath = path.Join(userHome, ".dapr/components")
		a.namespace=""
		loader = components.NewStandaloneComponents(a.runtimeConfig.Standalone)
	default:
		return errors.Errorf("components loader for mode %s not found", a.runtimeConfig.Mode)
	}

	log.Info("加载组件中")
	comps, err := loader.LoadComponents()
	if err != nil {
		return err
	}
	for _, comp := range comps {
		log.Debugf("found component. name: %s, type: %s/%s", comp.ObjectMeta.Name, comp.Spec.Type, comp.Spec.Version)
	}

	authorizedComps := a.getAuthorizedComponents(comps)

	a.componentsLock.Lock()
	a.components = make([]components_v1alpha1.Component, len(authorizedComps))
	copy(a.components, authorizedComps)
	a.componentsLock.Unlock()

	for _, comp := range authorizedComps {
		a.pendingComponents <- comp
	}

	return nil
}

// 添加或替换组件
func (a *DaprRuntime) appendOrReplaceComponents(component components_v1alpha1.Component) {
	a.componentsLock.Lock()
	defer a.componentsLock.Unlock()

	replaced := false
	for i, c := range a.components {
		if c.Spec.Type == component.Spec.Type && c.ObjectMeta.Name == component.Name {
			a.components[i] = component
			replaced = true
			break
		}
	}

	if !replaced {
		a.components = append(a.components, component)
	}
}

//提取组件类别
func (a *DaprRuntime) extractComponentCategory(component components_v1alpha1.Component) ComponentCategory {
	for _, category := range componentCategoriesNeedProcess {
		if strings.HasPrefix(component.Spec.Type, fmt.Sprintf("%s.", category)) {
			return category
		}
	}
	return ""
}

func (a *DaprRuntime) processComponents() {
	for comp := range a.pendingComponents {
		if comp.Name == "" {
			continue
		}

		err := a.processComponentAndDependents(comp)
		if err != nil {
			e := fmt.Sprintf("process component %s error: %s", comp.Name, err.Error())
			if !comp.Spec.IgnoreErrors {
				log.Warnf("process component error daprd process will exited, gracefully to stop")
				a.Shutdown(defaultGracefulShutdownDuration)
				log.Fatalf(e)
			}
			log.Errorf(e)
		}
	}
}

func (a *DaprRuntime) flushOutstandingComponents() {
	log.Info("等待所有未完成的组件被处理")
	// 我们通过发送一个无操作的组件进行冲洗。由于processComponents程序一次只能读取一个组件。
	// 我们知道，一旦从通道中读出no-op组件，之前的所有组件将被完全处理。
	//  pkg/runtime/runtime.go:239
	//pendingComponents 容量为0 ,者执行过去，说明之前的组件全部初始完毕
	a.pendingComponents <- components_v1alpha1.Component{}
	log.Info("所有未完成的部分都已处理")
}

func (a *DaprRuntime) getAuthorizedComponents(components []components_v1alpha1.Component) []components_v1alpha1.Component {
	authorized := []components_v1alpha1.Component{}

	for _, c := range components {
		if a.isComponentAuthorized(c) {
			authorized = append(authorized, c)
		}
	}
	return authorized
}

// 被依赖项就绪后，会依次调用会调用注册在pendingComponentDependents的下游组件
// 没有依赖项 被主动调用
/*
go processComponents(){
	for comp := range a.pendingComponents {
		a.processComponentAndDependents(comp)
	}
}

*
*/

func (a *DaprRuntime) processComponentAndDependents(comp components_v1alpha1.Component) error {
	log.Debugf("开始加载组件. name: %s, type: %s/%s", comp.ObjectMeta.Name, comp.Spec.Type, comp.Spec.Version)
	res := a.preprocessOneComponent(&comp) // 预处理组件,demo中组件没有元信息;故对此处没什么逻辑
	if res.unreadyDependency != "" {
		a.pendingComponentDependents[res.unreadyDependency] = append(a.pendingComponentDependents[res.unreadyDependency], comp)
		return nil
	}

	compCategory := a.extractComponentCategory(comp) // 提取组件的类型
	if compCategory == "" {
		// the category entered is incorrect, return error
		return errors.Errorf("incorrect type %s", comp.Spec.Type)
	}

	ch := make(chan error, 1)
	// 解析时间字符串,返回Duration类型
	timeout, err := time.ParseDuration(comp.Spec.InitTimeout)
	//timeout, err := time.ParseDuration("1h")
	if err != nil {
		timeout = defaultComponentInitTimeout // 默认5秒
	}
	// 判断组件初始超时
	go func() {
		ch <- a.doProcessOneComponent(compCategory, comp)
	}()

	select {
	case err := <-ch:
		if err != nil {
			return err
		}
	case <-time.After(timeout):
		return fmt.Errorf("组件的初始超时 %s 超过了 %s", comp.Name, timeout.String())
	}

	log.Infof("组件加载完成. name: %s, type: %s/%s", comp.ObjectMeta.Name, comp.Spec.Type, comp.Spec.Version)
	a.appendOrReplaceComponents(comp)
	// 记录指标
	diag.DefaultMonitoring.ComponentLoaded()

	dependency := componentDependency(compCategory, comp.Name) // secretstores:kubernetes
	if deps, ok := a.pendingComponentDependents[dependency]; ok {
		delete(a.pendingComponentDependents, dependency)
		for _, dependent := range deps {
			if err := a.processComponentAndDependents(dependent); err != nil {
				return err
			}
		}
	}

	return nil
}

// 程序初始化时，会根据全局配置 遍历每一个组件，进行初始化组件
// params 组件类型,组件本身
func (a *DaprRuntime) doProcessOneComponent(category ComponentCategory, comp components_v1alpha1.Component) error {
	switch category {
	case bindingsComponent:
		return a.initBinding(comp)
	case pubsubComponent:
		return a.initPubSub(comp) // ok
	case secretStoreComponent:
		return a.initSecretStore(comp) // ok
	case stateComponent:
		return a.initState(comp) // ok
	case configurationComponent:
		return a.initConfiguration(comp)
	}
	return nil
}

func (a *DaprRuntime) preprocessOneComponent(comp *components_v1alpha1.Component) componentPreprocessRes {
	var unreadySecretsStore string
	*comp, unreadySecretsStore = a.processComponentSecrets(*comp)
	if unreadySecretsStore != "" {
		return componentPreprocessRes{
			unreadyDependency: componentDependency(secretStoreComponent, unreadySecretsStore),
		}
	}
	return componentPreprocessRes{}
}

// 处理组件secret
func (a *DaprRuntime) processComponentSecrets(component components_v1alpha1.Component) (components_v1alpha1.Component, string) {
	cache := map[string]secretstores.GetSecretResponse{}

	for i, m := range component.Spec.Metadata {
		if m.SecretKeyRef.Name == "" {
			continue
		}

		secretStoreName := a.authSecretStoreOrDefault(component) // 获取存储
		secretStore := a.getSecretStore(secretStoreName)
		if secretStore == nil {
			log.Warnf("component %s references a secret store that isn't loaded: %s", component.Name, secretStoreName)
			return component, secretStoreName
		}

		// If running in Kubernetes, do not fetch secrets from the Kubernetes secret store as they will be populated by the operator.
		// Instead, base64 decode the secret values into their real self.
		if a.runtimeConfig.Mode == modes.KubernetesMode && secretStoreName == kubernetesSecretStore {
			val := m.Value.Raw

			var jsonVal string
			err := json.Unmarshal(val, &jsonVal)
			if err != nil {
				log.Errorf("error decoding secret: %s", err)
				continue
			}

			dec, err := base64.StdEncoding.DecodeString(jsonVal)
			if err != nil {
				log.Errorf("error decoding secret: %s", err)
				continue
			}

			m.Value = components_v1alpha1.DynamicValue{
				JSON: v1.JSON{
					Raw: dec,
				},
			}

			component.Spec.Metadata[i] = m
			continue
		}

		resp, ok := cache[m.SecretKeyRef.Name]
		if !ok {
			r, err := secretStore.GetSecret(secretstores.GetSecretRequest{
				Name: m.SecretKeyRef.Name,
				Metadata: map[string]string{
					"namespace": component.ObjectMeta.Namespace,
				},
			})
			if err != nil {
				log.Errorf("error getting secret: %s", err)
				continue
			}
			resp = r
		}

		// Use the SecretKeyRef.Name key if SecretKeyRef.Key is not given
		secretKeyName := m.SecretKeyRef.Key
		if secretKeyName == "" {
			secretKeyName = m.SecretKeyRef.Name
		}

		val, ok := resp.Data[secretKeyName]
		if ok {
			component.Spec.Metadata[i].Value = components_v1alpha1.DynamicValue{
				JSON: v1.JSON{
					Raw: []byte(val),
				},
			}
		}

		cache[m.SecretKeyRef.Name] = resp
	}
	return component, ""
}

// shutdownComponents 允许优雅地关闭所有运行时的内部操作或组件。
func (a *DaprRuntime) shutdownComponents() error {
	log.Info("Shutting down all components")
	var merr error

	// Close components if they implement `io.Closer`
	for name, binding := range a.inputBindings {
		if closer, ok := binding.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				err = fmt.Errorf("error closing input binding %s: %w", name, err)
				merr = multierror.Append(merr, err)
				log.Warn(err)
			}
		}
	}
	for name, binding := range a.outputBindings {
		if closer, ok := binding.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				err = fmt.Errorf("error closing output binding %s: %w", name, err)
				merr = multierror.Append(merr, err)
				log.Warn(err)
			}
		}
	}
	for name, secretstore := range a.secretStores {
		if closer, ok := secretstore.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				err = fmt.Errorf("error closing secret store %s: %w", name, err)
				merr = multierror.Append(merr, err)
				log.Warn(err)
			}
		}
	}
	for name, pubSub := range a.pubSubs {
		if err := pubSub.Close(); err != nil {
			err = fmt.Errorf("error closing pub sub %s: %w", name, err)
			merr = multierror.Append(merr, err)
			log.Warn(err)
		}
	}
	for name, stateStore := range a.stateStores {
		if closer, ok := stateStore.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				err = fmt.Errorf("error closing state store %s: %w", name, err)
				merr = multierror.Append(merr, err)
				log.Warn(err)
			}
		}
	}
	if closer, ok := a.nameResolver.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			err = fmt.Errorf("error closing name resolver: %w", err)
			merr = multierror.Append(merr, err)
			log.Warn(err)
		}
	}

	return merr
}

func (a *DaprRuntime) getComponent(componentType string, name string) (components_v1alpha1.Component, bool) {
	a.componentsLock.RLock()
	defer a.componentsLock.RUnlock()

	for i, c := range a.components {
		if c.Spec.Type == componentType && c.ObjectMeta.Name == name {
			return a.components[i], true
		}
	}
	return components_v1alpha1.Component{}, false
}

func (a *DaprRuntime) getComponents() []components_v1alpha1.Component {
	a.componentsLock.RLock()
	defer a.componentsLock.RUnlock()

	comps := make([]components_v1alpha1.Component, len(a.components))
	copy(comps, a.components)
	return comps
}

// 拼接组件依赖性键值
func componentDependency(compCategory ComponentCategory, name string) string {
	return fmt.Sprintf("%s:%s", compCategory, name)
}

func (a *DaprRuntime) beginComponentsUpdates() error {
	if a.runtimeConfig.Mode != modes.KubernetesMode {
		return nil
	}

	go func() {
		parseAndUpdate := func(compRaw []byte) {
			var component components_v1alpha1.Component
			if err := json.Unmarshal(compRaw, &component); err != nil {
				log.Warnf("error deserializing component: %s", err)
				return
			}

			if !a.isComponentAuthorized(component) {
				log.Debugf("received unauthorized component update, ignored. name: %s, type: %s/%s", component.ObjectMeta.Name, component.Spec.Type, component.Spec.Version)
				return
			}

			log.Debugf("received component update. name: %s, type: %s/%s", component.ObjectMeta.Name, component.Spec.Type, component.Spec.Version)
			a.onComponentUpdated(component)
		}

		needList := false
		for {
			var stream operatorv1pb.Operator_ComponentUpdateClient

			// 在流错误时重试。 指数退避
			backoff.Retry(func() error {
				var err error //在组件发生变化时，向Dapr sidecar发送事件。
				stream, err = a.operatorClient.ComponentUpdate(context.Background(), &operatorv1pb.ComponentUpdateRequest{
					Namespace: a.namespace,
				})
				if err != nil {
					log.Errorf("error from operator stream: %s", err)
					return err
				}
				return nil
			}, backoff.NewExponentialBackOff()) //  使用默认值创建一个ExponentialBackOff的实例。

			if needList {
				// 我们应该再次获得所有的组件，以避免在故障时间内错过任何更新。
				backoff.Retry(func() error {
					resp, err := a.operatorClient.ListComponents(context.Background(), &operatorv1pb.ListComponentsRequest{
						Namespace: a.namespace,
					})
					if err != nil {
						log.Errorf("error listing components: %s", err)
						return err
					}

					comps := resp.GetComponents()
					for i := 0; i < len(comps); i++ {
						// avoid missing any updates during the init component time.
						go func(comp []byte) {
							parseAndUpdate(comp)
						}(comps[i])
					}

					return nil
				}, backoff.NewExponentialBackOff())
			}

			for {
				c, err := stream.Recv() // 接收到来自operator的消息，阻塞
				if err != nil {
					// Retry on stream error.
					needList = true
					log.Errorf("error from operator stream: %s", err)
					break
				}

				parseAndUpdate(c.GetComponent())
			}
		}
	}()
	return nil
}

func (a *DaprRuntime) onComponentUpdated(component components_v1alpha1.Component) {
	oldComp, exists := a.getComponent(component.Spec.Type, component.Name)
	if exists && reflect.DeepEqual(oldComp.Spec.Metadata, component.Spec.Metadata) {
		return
	}
	a.pendingComponents <- component
}
func (a *DaprRuntime) initConfiguration(s components_v1alpha1.Component) error {
	store, err := a.configurationStoreRegistry.Create(s.Spec.Type, s.Spec.Version)
	if err != nil {
		log.Warnf("error creating configuration store %s (%s/%s): %s", s.ObjectMeta.Name, s.Spec.Type, s.Spec.Version, err)
		diag.DefaultMonitoring.ComponentInitFailed(s.Spec.Type, "creation")
		return err
	}
	if store != nil {
		props := a.convertMetadataItemsToProperties(s.Spec.Metadata)
		err := store.Init(configuration.Metadata{
			Properties: props,
		})
		if err != nil {
			diag.DefaultMonitoring.ComponentInitFailed(s.Spec.Type, "init")
			log.Warnf("error initializing configuration store %s (%s/%s): %s", s.ObjectMeta.Name, s.Spec.Type, s.Spec.Version, err)
			return err
		}

		a.configurationStores[s.ObjectMeta.Name] = store
		diag.DefaultMonitoring.ComponentInitialized(s.Spec.Type)
	}

	return nil
}
