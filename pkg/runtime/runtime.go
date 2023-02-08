// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package runtime

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dapr/components-contrib/configuration"
	configuration_loader "github.com/dapr/dapr/pkg/components/configuration"

	"contrib.go.opencensus.io/exporter/zipkin"
	"github.com/dapr/components-contrib/bindings"
	"github.com/dapr/components-contrib/middleware"
	nr "github.com/dapr/components-contrib/nameresolution"
	"github.com/dapr/components-contrib/pubsub"
	"github.com/dapr/components-contrib/secretstores"
	"github.com/dapr/components-contrib/state"
	"github.com/dapr/dapr/pkg/actors"
	components_v1alpha1 "github.com/dapr/dapr/pkg/apis/components/v1alpha1"
	"github.com/dapr/dapr/pkg/channel"
	http_channel "github.com/dapr/dapr/pkg/channel/http"
	bindings_loader "github.com/dapr/dapr/pkg/components/bindings"
	http_middleware_loader "github.com/dapr/dapr/pkg/components/middleware/http"
	nr_loader "github.com/dapr/dapr/pkg/components/nameresolution"
	pubsub_loader "github.com/dapr/dapr/pkg/components/pubsub"
	secretstores_loader "github.com/dapr/dapr/pkg/components/secretstores"
	state_loader "github.com/dapr/dapr/pkg/components/state"
	"github.com/dapr/dapr/pkg/config"
	diag "github.com/dapr/dapr/pkg/diagnostics"
	diag_utils "github.com/dapr/dapr/pkg/diagnostics/utils"
	"github.com/dapr/dapr/pkg/grpc"
	"github.com/dapr/dapr/pkg/http"
	"github.com/dapr/dapr/pkg/messaging"
	http_middleware "github.com/dapr/dapr/pkg/middleware/http"
	"github.com/dapr/dapr/pkg/modes"
	"github.com/dapr/dapr/pkg/operator/client"
	operatorv1pb "github.com/dapr/dapr/pkg/proto/operator/v1"
	runtime_pubsub "github.com/dapr/dapr/pkg/runtime/pubsub"
	"github.com/dapr/dapr/pkg/runtime/security"
	"github.com/dapr/dapr/utils"
	"github.com/dapr/kit/logger"
	"github.com/google/uuid"
	jsoniter "github.com/json-iterator/go"
	openzipkin "github.com/openzipkin/zipkin-go"
	zipkinreporter "github.com/openzipkin/zipkin-go/reporter/http"
	"github.com/pkg/errors"
)

const (
	actorStateStore = "actorStateStore"

	// 输出绑定的并发性。
	bindingsConcurrencyParallel   = "parallel"
	bindingsConcurrencySequential = "sequential"
	pubsubName                    = "pubsubName"
)

var log = logger.NewLogger("dapr.runtime")

// ErrUnexpectedEnvelopeData denotes that an unexpected data type
// was encountered when processing a cloud event's data property.
var ErrUnexpectedEnvelopeData = errors.New("unexpected data type encountered in envelope")

type Route struct {
	metadata map[string]string
	rules    []*runtime_pubsub.Rule
}

type TopicRoute struct {
	routes map[string]Route
}

// DaprRuntime 持有运行时的所有核心组件。
type DaprRuntime struct {
	runtimeConfig     *Config                         // 运行时配置
	globalConfig      *config.Configuration           // k8s中的全局配置
	accessControlList *config.AccessControlList       // 访问控制列表
	componentsLock    *sync.RWMutex                   // 组件锁
	components        []components_v1alpha1.Component // 所有组件
	grpc              *grpc.Manager
	appConfig         config.ApplicationConfig  // 应用配置
	directMessaging   messaging.DirectMessaging // todo

	stateStoreRegistry state_loader.Registry  // 状态工厂map
	stateStores        map[string]state.Store //  state实例map
	actor              actors.Actors

	nameResolutionRegistry nr_loader.Registry // 名称解析工厂map

	bindingsRegistry bindings_loader.Registry          // 输入输出binging 工厂map
	inputBindings    map[string]bindings.InputBinding  // InputBinding 实例
	outputBindings   map[string]bindings.OutputBinding // OutputBinding 实例

	secretStoresRegistry secretstores_loader.Registry // sceret工厂map
	secretStores         map[string]secretstores.SecretStore

	pubSubRegistry pubsub_loader.Registry // pubsub工厂map
	//	pkg/runtime/runtime.go:1367
	pubSubs              map[string]pubsub.PubSub // pubsub 实例
	subscribeBindingList []string

	nameResolver           nr.Resolver
	json                   jsoniter.API
	httpMiddlewareRegistry http_middleware_loader.Registry // 中间件工厂map
	hostAddress            string
	actorStateStoreName    string // 将yaml中设置了actorStateStore 保存到这里，【名字】
	actorStateStoreCount   int
	authenticator          security.Authenticator
	namespace              string
	scopedSubscriptions    map[string][]string
	scopedPublishings      map[string][]string
	allowedTopics          map[string][]string
	appChannel             channel.AppChannel // 并不是go的channel ,是一层抽象,与APP之间的通信
	daprHTTPAPI            http.API

	operatorClient     operatorv1pb.OperatorClient // operator = controlPlaneAddress = dapr-api.dapr-system.svc.cluster.local:80 == localhost:6500
	topicRoutes        map[string]TopicRoute
	inputBindingRoutes map[string]string
	shutdownC          chan error
	apiClosers         []io.Closer // 可关闭的服务，1、dapr对app暴露的http 2、dapr对app暴露的grpc 3、dapr对dapr暴露的grpc

	secretsConfiguration map[string]config.SecretsScope

	configurationStoreRegistry configuration_loader.Registry // 配置工厂map
	configurationStores        map[string]configuration.Store

	pendingComponents          chan components_v1alpha1.Component         // 等待初始化的组件
	pendingComponentDependents map[string][]components_v1alpha1.Component // 组件依赖前置, 当该组件就绪后，会主动从此map去掉

	proxy messaging.Proxy

	// TODO: Remove feature flag once feature is ratified
	//一旦功能被批准，删除功能标志
	featureRoutingEnabled bool
}

type pubsubSubscribedMessage struct {
	cloudEvent map[string]interface{}
	data       []byte
	topic      string
	metadata   map[string]string
	path       string
}

// NewDaprRuntime 返回一个具有给定运行时配置和全局配置的新运行时。
func NewDaprRuntime(runtimeConfig *Config, globalConfig *config.Configuration, accessControlList *config.AccessControlList) *DaprRuntime {
	return &DaprRuntime{
		runtimeConfig:          runtimeConfig,     // 传递过来的参数
		globalConfig:           globalConfig,      // k8s 的appconfig
		accessControlList:      accessControlList, // 访问控制列表，公司场景为nil
		componentsLock:         &sync.RWMutex{},
		components:             make([]components_v1alpha1.Component, 0),
		grpc:                   grpc.NewGRPCManager(runtimeConfig.Mode),
		json:                   jsoniter.ConfigFastest,
		inputBindings:          map[string]bindings.InputBinding{},
		outputBindings:         map[string]bindings.OutputBinding{},
		secretStores:           map[string]secretstores.SecretStore{},
		stateStores:            map[string]state.Store{},
		pubSubs:                map[string]pubsub.PubSub{},
		stateStoreRegistry:     state_loader.NewRegistry(),
		bindingsRegistry:       bindings_loader.NewRegistry(),
		pubSubRegistry:         pubsub_loader.NewRegistry(),
		secretStoresRegistry:   secretstores_loader.NewRegistry(),
		nameResolutionRegistry: nr_loader.NewRegistry(),
		httpMiddlewareRegistry: http_middleware_loader.NewRegistry(),

		scopedSubscriptions: map[string][]string{},
		scopedPublishings:   map[string][]string{},
		allowedTopics:       map[string][]string{},
		inputBindingRoutes:  map[string]string{},

		secretsConfiguration:       map[string]config.SecretsScope{},
		configurationStoreRegistry: configuration_loader.NewRegistry(),
		configurationStores:        map[string]configuration.Store{},

		pendingComponents:          make(chan components_v1alpha1.Component),
		pendingComponentDependents: map[string][]components_v1alpha1.Component{},
		shutdownC:                  make(chan error, 1),
	}
}

// Run performs initialization of the runtime with the runtime and global configurations.
// 用运行时和全局配置执行运行时的初始化。
func (a *DaprRuntime) Run(opts ...Option) error {
	start := time.Now().UTC()
	log.Infof("%s mode configured", a.runtimeConfig.Mode)
	log.Infof("app id: %s", a.runtimeConfig.ID)

	var o runtimeOpts
	for _, opt := range opts {
		opt(&o)
	}

	err := a.initRuntime(&o)
	if err != nil {
		return err
	}

	d := time.Since(start).Seconds() * 1000
	log.Infof("dapr初始化。状态:运行。初始化运行%vms", d)

	if a.daprHTTPAPI != nil {
		// 在initRuntime方法中将gRPC服务器启动失败记录为Fatal。仅在初始化运行时时设置状态。
		a.daprHTTPAPI.MarkStatusAsReady()
	}

	return nil
}

// 生成dapr-api服务的链接=客户端
func (a *DaprRuntime) getOperatorClient() (operatorv1pb.OperatorClient, error) {
	if a.runtimeConfig.Mode == modes.KubernetesMode {
		//dapr-api.dapr-system.svc.cluster.local:80
		//"cluster.local"
		client, _, err := client.GetOperatorClient(a.runtimeConfig.Kubernetes.ControlPlaneAddress, security.TLSServerName, a.runtimeConfig.CertChain)
		if err != nil {
			return nil, errors.Wrap(err, "error creating operator client")
		}
		return client, nil
	}
	return nil, nil
}

// setupTracing
// 设置跟踪输出程序。技术上来说，我们不需要传递`hostAddress`
// 但我们在这里这样做是为了明确指出对`hostAddress`计算的依赖性。
func (a *DaprRuntime) setupTracing(hostAddress string, exporters traceExporterStore) error {
	// 如果用户想对请求进行调试或作为信息级别的日志，注册stdout跟踪输出器。
	if a.globalConfig.Spec.TracingSpec.Stdout {
		// 全局的,也是opencensus的,span信息也会打印到终端
		exporters.RegisterExporter(&diag_utils.StdoutExporter{})
	}

	// 如果指定了ZipkinSpec，则注册zipkin跟踪输出器 http://zipkin.mesoid.svc.cluster.local:9411/api/v2/spans
	if a.globalConfig.Spec.TracingSpec.Zipkin.EndpointAddress != "" {
		// 简单判断一下传入的ip是否合规
		localEndpoint, err := openzipkin.NewEndpoint(a.runtimeConfig.ID, hostAddress) // dp-618b5e4aa5ebc3924db86860-executorapp,10.10.16.115
		if err != nil {
			return err
		}
		// zipkin span接收器, 启动了goroutinue 用于往zipkin发信息
		reporter := zipkinreporter.NewReporter(a.globalConfig.Spec.TracingSpec.Zipkin.EndpointAddress)
		exporter := zipkin.NewExporter(reporter, localEndpoint)
		exporters.RegisterExporter(exporter)
	}
	return nil
}

func (a *DaprRuntime) initRuntime(opts *runtimeOpts) error {
	// 只有当MetricSpec被启用时，才会初始化度量指标，默认为true
	if a.globalConfig.Spec.MetricSpec.Enabled {
		// 初始化各种指标 grpc|http|runtime
		if err := diag.InitMetrics(a.runtimeConfig.ID); err != nil {
			log.Errorf("failed to initialize metrics: %v", err)
		}
	}
	// dapr-sentry.dapr-system.svc.cluster.local:80
	// grpc 通信设置证书
	err := a.establishSecurity(a.runtimeConfig.SentryServiceAddress)
	if err != nil {
		return err
	}
	// 环境变量中获取
	a.namespace = a.getNamespace()
	// kubectl port-forward svc/dapr-api -n dapr-system 6500:80 &
	// operator = controlPlaneAddress = dapr-api.dapr-system.svc.cluster.local:80 == localhost:6500
	a.operatorClient, err = a.getOperatorClient()
	if err != nil {
		return err
	}
	// 获取本机的IP
	if a.hostAddress, err = utils.GetHostAddress(); err != nil {
		return errors.Wrap(err, "failed to determine host address")
	}
	// 会启动到zipkin的导出器
	if err = a.setupTracing(a.hostAddress, openCensusExporterStore{}); err != nil {
		return errors.Wrap(err, "failed to setup tracing")
	}
	// 注册并初始化用于服务发现的名称解析。,默认是注册了三个 ，mdns,k8s,consul
	a.nameResolutionRegistry.Register(opts.nameResolutions...)
	err = a.initNameResolution()
	if err != nil {
		log.Warnf("初始化域名解析失败: %s", err)
	}

	a.pubSubRegistry.Register(opts.pubsubs...)
	a.secretStoresRegistry.Register(opts.secretStores...)
	a.stateStoreRegistry.Register(opts.states...)
	a.configurationStoreRegistry.Register(opts.configurations...)
	a.bindingsRegistry.RegisterInputBindings(opts.inputBindings...)
	a.bindingsRegistry.RegisterOutputBindings(opts.outputBindings...)
	a.httpMiddlewareRegistry.Register(opts.httpMiddleware...)

	// 怎么保证这个go在appendBuiltinSecretStore 添加了k8s secret组件后执行, 因为它是chan
	go a.processComponents() // 遍历 pendingComponents
	// 处理来自operator的事件流，开始组件更新,
	err = a.beginComponentsUpdates()
	if err != nil {
		log.Warnf("failed to watch component updates: %s", err)
	}
	a.appendBuiltinSecretStore() //向 pendingComponents 添加内置的secret存储, k8s才会触发
	err = a.loadComponents(opts) // 使用不同的loader加载不同的组件， 主要是获取 认证组件，并将其发往 pendingComponents
	if err != nil {
		log.Warnf("failed to load components: %s", err)
	}
	//等待所有未完成的组件被处理
	a.flushOutstandingComponents()
	//构建用于fasthttp的中间件
	pipeline, err := a.buildHTTPPipeline() // 大多数都是有关认证的组件
	if err != nil {
		log.Warnf("failed to build HTTP pipeline: %s", err)
	}

	// 设置 allow/deny list for secrets
	a.populateSecretsConfiguration()

	// Start proxy
	a.initProxy() //

	// 创建和启动内部和外部gRPC服务器 、struct
	grpcAPI := a.getGRPCAPI() // 承接app流量的实现
	// 50001
	// 1、外部grpc
	err = a.startGRPCAPIServer(grpcAPI, a.runtimeConfig.APIGRPCPort)
	if err != nil {
		log.Fatalf("failed to start API gRPC server: %s", err)
	}
	if a.runtimeConfig.UnixDomainSocket != "" {
		log.Info("API gRPC server is 运行在了unix域套接字")
	} else {
		log.Infof("API gRPC server is running on port %v", a.runtimeConfig.APIGRPCPort)
	}
	// 2、外部http
	// Start HTTP Server 【http  prof】
	err = a.startHTTPServer(a.runtimeConfig.HTTPPort, a.runtimeConfig.PublicPort, a.runtimeConfig.ProfilePort, a.runtimeConfig.AllowedOrigins, pipeline)
	if err != nil {
		log.Fatalf("failed to start HTTP server: %s", err)
	}
	if a.runtimeConfig.UnixDomainSocket != "" {
		log.Info("http server is 运行在了unix域套接字")
	} else {
		log.Infof("http server is running on port %v", a.runtimeConfig.HTTPPort)
	}
	log.Infof("The request body size parameter is: %v", a.runtimeConfig.MaxRequestBodySize)
	// 3、内部grpc
	err = a.startGRPCInternalServer(grpcAPI, a.runtimeConfig.InternalGRPCPort)
	if err != nil {
		log.Fatalf("failed to start internal gRPC server: %s", err)
	}
	log.Infof("internal gRPC server is running on port %v", a.runtimeConfig.InternalGRPCPort)

	if a.daprHTTPAPI != nil {
		a.daprHTTPAPI.MarkStatusAsOutboundReady()
	}

	a.blockUntilAppIsReady()

	err = a.createAppChannel()
	if err != nil {
		log.Warnf("failed to open %s channel to app: %s", string(a.runtimeConfig.ApplicationProtocol), err)
	}
	// 将创建的channel 与api绑定  , 与应用协议绑定，只能创建一个channel
	a.daprHTTPAPI.SetAppChannel(a.appChannel) //
	grpcAPI.SetAppChannel(a.appChannel)

	a.loadAppConfiguration()
	// 封装了k8s域名解析组件的工厂函数
	a.initDirectMessaging(a.nameResolver)

	// 将directMessaging封装到http、grpc
	a.daprHTTPAPI.SetDirectMessaging(a.directMessaging)
	grpcAPI.SetDirectMessaging(a.directMessaging)

	err = a.initActors()
	if err != nil {
		log.Warnf("failed to init actors: %s", err)
	}
	time.Sleep(time.Second)
	a.daprHTTPAPI.SetActorRuntime(a.actor)
	grpcAPI.SetActorRuntime(a.actor)

	// TODO: Remove feature flag once feature is ratified
	// 一旦功能被批准，删除功能标志
	a.featureRoutingEnabled = config.IsFeatureEnabled(a.globalConfig.Spec.Features, config.PubSubRouting)

	if opts.componentsCallback != nil {
		if err = opts.componentsCallback(ComponentRegistry{
			Actors:          a.actor,
			DirectMessaging: a.directMessaging,
			StateStores:     a.stateStores,
			InputBindings:   a.inputBindings,
			OutputBindings:  a.outputBindings,
			SecretStores:    a.secretStores,
			PubSubs:         a.pubSubs,
		}); err != nil {
			log.Fatalf("failed to register components with callback: %s", err)
		}
	}
	// 启动订阅bing
	a.startSubscribing()
	err = a.startReadingFromBindings() // 从输入binding读取消息
	if err != nil {
		log.Warnf("从输入绑定读取数据失败: %s ", err)
	}
	return nil
}

func (a *DaprRuntime) populateSecretsConfiguration() {
	// Populate in a map for easy lookup by store name.
	// 填充在map中，便于按store名称查找。
	for _, scope := range a.globalConfig.Spec.Secrets.Scopes {
		a.secretsConfiguration[scope.StoreName] = scope
	}
}

// 构建用于fasthttp的中间件
func (a *DaprRuntime) buildHTTPPipeline() (http_middleware.Pipeline, error) {
	var handlers []http_middleware.Middleware

	if a.globalConfig != nil {
		for i := 0; i < len(a.globalConfig.Spec.HTTPPipelineSpec.Handlers); i++ {
			middlewareSpec := a.globalConfig.Spec.HTTPPipelineSpec.Handlers[i]
			component, exists := a.getComponent(middlewareSpec.Type, middlewareSpec.Name)
			if !exists {
				return http_middleware.Pipeline{}, errors.Errorf("couldn't find middleware component with name %s and type %s/%s",
					middlewareSpec.Name,
					middlewareSpec.Type,
					middlewareSpec.Version)
			}
			handler, err := a.httpMiddlewareRegistry.Create(middlewareSpec.Type, middlewareSpec.Version,
				middleware.Metadata{Properties: a.convertMetadataItemsToProperties(component.Spec.Metadata)})
			if err != nil {
				return http_middleware.Pipeline{}, err
			}
			log.Infof("enabled %s/%s http middleware", middlewareSpec.Type, middlewareSpec.Version)
			handlers = append(handlers, handler)
		}
	}
	return http_middleware.Pipeline{Handlers: handlers}, nil
}

// findMatchingRoute 根据路由规则选择路径。如果没有匹配的规则，则使用路由级路径。
func findMatchingRoute(route *Route, cloudEvent interface{}, routingEnabled bool) (path string, shouldProcess bool, err error) {
	hasRules := len(route.rules) > 0
	if hasRules {
		data := map[string]interface{}{
			"event": cloudEvent,
		}
		rule, err := matchRoutingRule(route, data, routingEnabled)
		if err != nil {
			return "", false, err
		}
		if rule != nil {
			return rule.Path, true, nil
		}
	}

	return "", false, nil
}

func matchRoutingRule(route *Route, data map[string]interface{}, routingEnabled bool) (*runtime_pubsub.Rule, error) {
	for _, rule := range route.rules {
		if rule.Match == nil {
			return rule, nil
		}
		// 如果未启用路由，则跳过匹配评估。
		if !routingEnabled {
			continue
		}
		iResult, err := rule.Match.Eval(data)
		if err != nil {
			return nil, err
		}
		result, ok := iResult.(bool)
		if !ok {
			return nil, errors.Errorf("匹配表达的结果 %s was not a boolean", rule.Match)
		}

		if result {
			return rule, nil
		}
	}

	return nil, nil
}

func (a *DaprRuntime) initProxy() {
	// TODO: remove feature check once stable
	// 稳定后取消功能检查 , 是否对proxy.grpc设置了代理
	if config.IsFeatureEnabled(a.globalConfig.Spec.Features, messaging.GRPCFeatureName) {
		a.proxy = messaging.NewProxy(a.grpc.GetGRPCConnection, a.runtimeConfig.ID,
			fmt.Sprintf("%s:%d", channel.DefaultChannelAddress, a.runtimeConfig.ApplicationPort),
			a.runtimeConfig.InternalGRPCPort, a.accessControlList,
		)
		//   127.0.0.1:3001
		//  50002
		log.Info("gRPC proxy enabled")
	}
}

func (a *DaprRuntime) onAppResponse(response *bindings.AppResponse) error {
	if len(response.State) > 0 {
		go func(reqs []state.SetRequest) {
			if a.stateStores != nil {
				err := a.stateStores[response.StoreName].BulkSet(reqs)
				if err != nil {
					log.Errorf("error saving state from app response: %s", err)
				}
			}
		}(response.State)
	}

	if len(response.To) > 0 {
		b, err := a.json.Marshal(&response.Data)
		if err != nil {
			return err
		}

		if response.Concurrency == bindingsConcurrencyParallel {
			a.sendBatchOutputBindingsParallel(response.To, b)
		} else {
			return a.sendBatchOutputBindingsSequential(response.To, b)
		}
	}

	return nil
}

func (a *DaprRuntime) startHTTPServer(port int, publicPort *int, profilePort int, allowedOrigins string, pipeline http_middleware.Pipeline) error {
	a.daprHTTPAPI = http.NewAPI(a.runtimeConfig.ID, a.appChannel, a.directMessaging, a.getComponents, a.stateStores, a.secretStores,
		a.secretsConfiguration, a.getPublishAdapter(), a.actor, a.sendToOutputBinding, a.globalConfig.Spec.TracingSpec, a.ShutdownWithWait)
	serverConf := http.NewServerConfig(a.runtimeConfig.ID, a.hostAddress, port, a.runtimeConfig.APIListenAddresses, publicPort, profilePort, allowedOrigins, a.runtimeConfig.EnableProfiling, a.runtimeConfig.MaxRequestBodySize, a.runtimeConfig.UnixDomainSocket, a.runtimeConfig.ReadBufferSize, a.runtimeConfig.StreamRequestBody)

	server := http.NewServer(a.daprHTTPAPI, serverConf, a.globalConfig.Spec.TracingSpec, a.globalConfig.Spec.MetricSpec, pipeline, a.globalConfig.Spec.APISpec)
	//pkg/http/server.go:67
	if err := server.StartNonBlocking(); err != nil {
		return err
	}
	a.apiClosers = append(a.apiClosers, server)

	return nil
}

// daprd 间通信的服务
func (a *DaprRuntime) startGRPCInternalServer(api grpc.API, port int) error {
	// 由于GRPCInteralServer是经过加密和认证的，所以它是安全的，可以监听*。
	serverConf := a.getNewServerConfig([]string{""}, port) // 对外暴露
	server := grpc.NewInternalServer(api, serverConf, a.globalConfig.Spec.TracingSpec, a.globalConfig.Spec.MetricSpec, a.authenticator, a.proxy)
	if err := server.StartNonBlocking(); err != nil {
		return err
	}
	a.apiClosers = append(a.apiClosers, server)

	return nil
}

// 对app暴露的服务
func (a *DaprRuntime) startGRPCAPIServer(api grpc.API, port int) error {
	serverConf := a.getNewServerConfig(a.runtimeConfig.APIListenAddresses, port) // 只对本地暴露
	// struct
	server := grpc.NewAPIServer(api, serverConf, a.globalConfig.Spec.TracingSpec, a.globalConfig.Spec.MetricSpec, a.globalConfig.Spec.APISpec, a.proxy)
	if err := server.StartNonBlocking(); err != nil {
		return err
	}
	a.apiClosers = append(a.apiClosers, server)

	return nil
}

func (a *DaprRuntime) getNewServerConfig(apiListenAddresses []string, port int) grpc.ServerConfig {
	// 使用访问控制策略规范中的信任域值来生成证书 如果没有指定访问控制策略，使用默认值
	trustDomain := config.DefaultTrustDomain
	if a.accessControlList != nil {
		trustDomain = a.accessControlList.TrustDomain
	}
	return grpc.NewServerConfig(a.runtimeConfig.ID, a.hostAddress, port, apiListenAddresses, a.namespace, trustDomain, a.runtimeConfig.MaxRequestBodySize, a.runtimeConfig.UnixDomainSocket, a.runtimeConfig.ReadBufferSize)
}

func (a *DaprRuntime) getGRPCAPI() grpc.API {
	// dp-618b5e4aa5ebc3924db86860-executorapp, nil,map,map,map,map
	//nil,nil,nil
	//func,zipkin,nil,http
	// func,func  创建了一个结构体
	return grpc.NewAPI(
		a.runtimeConfig.ID, a.appChannel, a.stateStores, a.secretStores, a.secretsConfiguration, a.configurationStores,
		a.getPublishAdapter(), a.directMessaging, a.actor,
		a.sendToOutputBinding, a.globalConfig.Spec.TracingSpec, a.accessControlList, string(a.runtimeConfig.ApplicationProtocol),
		a.getComponents, a.ShutdownWithWait,
	)
}

// ShutdownWithWait 将优雅地停止runtime，等待未完成的操作。
func (a *DaprRuntime) ShutdownWithWait() {
	a.Shutdown(defaultGracefulShutdownDuration) // 5 秒
	os.Exit(0)
}

func (a *DaprRuntime) cleanSocket() {
	if a.runtimeConfig.UnixDomainSocket != "" {
		for _, s := range []string{"http", "grpc"} {
			os.Remove(fmt.Sprintf("%s/dapr-%s-%s.socket", a.runtimeConfig.UnixDomainSocket, a.runtimeConfig.ID, s))
		}
	}
}

func (a *DaprRuntime) Shutdown(duration time.Duration) {
	// 如果发生panic，确保Unix套接字文件被删除。
	defer a.cleanSocket()
	a.stopActor()
	log.Infof("dapr shutting down.")
	log.Info("Stopping Dapr APIs")
	for _, closer := range a.apiClosers {
		if err := closer.Close(); err != nil {
			log.Warnf("error closing API: %v", err)
		}
	}
	log.Infof("等待%s来完成未完成的操作", duration)
	<-time.After(duration)
	a.shutdownComponents()
	a.shutdownC <- nil
}

func (a *DaprRuntime) WaitUntilShutdown() error {
	return <-a.shutdownC
}

// 阻塞直到应用就绪
func (a *DaprRuntime) blockUntilAppIsReady() {
	if a.runtimeConfig.ApplicationPort <= 0 {
		return
	}

	log.Infof("application protocol: %s. waiting on port %v.  阻塞直到应用就绪", string(a.runtimeConfig.ApplicationProtocol), a.runtimeConfig.ApplicationPort)

	for {
		conn, _ := net.DialTimeout("tcp", net.JoinHostPort("localhost", fmt.Sprintf("%v", a.runtimeConfig.ApplicationPort)), time.Millisecond*500)
		if conn != nil {
			conn.Close()
			break
		}
		// 防止连接使操作系统不堪重负
		time.Sleep(time.Millisecond * 50)
	}

	log.Infof("发现应用在端口:%v", a.runtimeConfig.ApplicationPort)
}

func (a *DaprRuntime) loadAppConfiguration() {
	if a.appChannel == nil {
		return
	}

	appConfig, err := a.appChannel.GetAppConfig()
	if err != nil {
		return
	}

	if appConfig != nil {
		a.appConfig = *appConfig
		log.Info("应用配置已加载")
	}
}

// 创建与app 通信的通道
// 如果是http ，创建http client
// 如果是grpc, 与app 建链
func (a *DaprRuntime) createAppChannel() error {
	if a.runtimeConfig.ApplicationPort > 0 {
		var channelCreatorFn func(port, maxConcurrency int, spec config.TracingSpec, sslEnabled bool, maxRequestBodySize int, readBufferSize int) (channel.AppChannel, error)

		switch a.runtimeConfig.ApplicationProtocol {
		case GRPCProtocol:
			channelCreatorFn = a.grpc.CreateLocalChannel
		case HTTPProtocol:
			channelCreatorFn = http_channel.CreateLocalChannel
		default:
			return errors.Errorf("cannot create app channel for protocol %s", string(a.runtimeConfig.ApplicationProtocol))
		}

		ch, err := channelCreatorFn(a.runtimeConfig.ApplicationPort,
			a.runtimeConfig.MaxConcurrency,
			a.globalConfig.Spec.TracingSpec,
			a.runtimeConfig.AppSSL, // 是否加密
			a.runtimeConfig.MaxRequestBodySize,
			a.runtimeConfig.ReadBufferSize,
		)
		if err != nil {
			log.Infof("app max concurrency set to %v", a.runtimeConfig.MaxConcurrency)
		}
		a.appChannel = ch
	}

	return nil
}

//将用户填写的spec.metadata 中包含{uuid}的键值对 转换成属性
func (a *DaprRuntime) convertMetadataItemsToProperties(items []components_v1alpha1.MetadataItem) map[string]string {
	properties := map[string]string{}
	for _, c := range items {
		val := c.Value.String()
		for strings.Contains(val, "{uuid}") {
			val = strings.Replace(val, "{uuid}", uuid.New().String(), 1)
		}
		properties[c.Name] = val
	}
	return properties
}

// 确立安全
func (a *DaprRuntime) establishSecurity(sentryAddress string) error {
	if !a.runtimeConfig.mtlsEnabled {
		log.Info("mTLS被禁用。跳过证书请求和tls验证")
		return nil
	}
	if sentryAddress == "" {
		return errors.New("sentryAddress不能为空")
	}
	log.Info("启用mTLS，创建sidecar认证器")
	// daprd 运行时的证书，从sidecar环境变量中获取的
	// code_debug/daprd/daprd_debug.go:57
	// dapr-sentry.dapr-system.svc.cluster.local:80
	auth, err := security.GetSidecarAuthenticator(sentryAddress, a.runtimeConfig.CertChain)
	if err != nil {
		return err
	}
	a.authenticator = auth
	a.grpc.SetAuthenticator(auth)
	log.Info("创建认证器")

	// 记录指标
	diag.DefaultMonitoring.MTLSInitCompleted()
	return nil
}
