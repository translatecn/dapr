package runtime

import (
	"github.com/dapr/dapr/pkg/components/bindings"        // 规定了 InputBinding 、OutputBinding 需要传递哪些配置 [名字、构造方法]
	"github.com/dapr/dapr/pkg/components/configuration"   // 规定了 Configuration  需要传递哪些配置
	"github.com/dapr/dapr/pkg/components/middleware/http" // 规定了 Middleware 需要传递哪些配置
	"github.com/dapr/dapr/pkg/components/nameresolution"  // 规定了 NameResolution 需要传递哪些配置
	"github.com/dapr/dapr/pkg/components/pubsub"          // 规定了 PubSub 需要传递哪些配置
	"github.com/dapr/dapr/pkg/components/secretstores"    // 规定了 SecretStore 需要传递哪些配置
	"github.com/dapr/dapr/pkg/components/state"           // 规定了 State 需要传递哪些配置
)

type (
	// runtimeOpts  封装了要包含在运行时中的组件。
	runtimeOpts struct {
		secretStores    []secretstores.SecretStore
		states          []state.State
		configurations  []configuration.Configuration
		pubsubs         []pubsub.PubSub
		nameResolutions []nameresolution.NameResolution
		inputBindings   []bindings.InputBinding
		outputBindings  []bindings.OutputBinding
		httpMiddleware  []http.Middleware

		componentsCallback ComponentsCallback
	}

	// Option 扩展 运行时各个组件的 功能
	//is a function that customizes the runtime.
	Option func(o *runtimeOpts)
)

// WithSecretStores 向runtime添加秘钥存储组件
func WithSecretStores(secretStores ...secretstores.SecretStore) Option {
	return func(o *runtimeOpts) {
		o.secretStores = append(o.secretStores, secretStores...)
	}
}

// WithStates 向runtime添加状态存储组件
func WithStates(states ...state.State) Option {
	return func(o *runtimeOpts) {
		o.states = append(o.states, states...)
	}
}

// WithConfigurations 向runtime添加配置存储组件
func WithConfigurations(configurations ...configuration.Configuration) Option {
	return func(o *runtimeOpts) {
		o.configurations = append(o.configurations, configurations...)
	}
}

// WithPubSubs 向runtime添加发布订阅组件
func WithPubSubs(pubsubs ...pubsub.PubSub) Option {
	return func(o *runtimeOpts) {
		o.pubsubs = append(o.pubsubs, pubsubs...)
	}
}

// WithNameResolutions 添加名称解析组件
func WithNameResolutions(nameResolutions ...nameresolution.NameResolution) Option {
	return func(o *runtimeOpts) {
		o.nameResolutions = append(o.nameResolutions, nameResolutions...)
	}
}

// WithInputBindings 添加获取消息的 组件 [可以理解为消费者]
func WithInputBindings(inputBindings ...bindings.InputBinding) Option {
	return func(o *runtimeOpts) {
		o.inputBindings = append(o.inputBindings, inputBindings...)
	}
}

// WithOutputBindings 添加发布消息的组件 [可以理解为生产者]
func WithOutputBindings(outputBindings ...bindings.OutputBinding) Option {
	return func(o *runtimeOpts) {
		o.outputBindings = append(o.outputBindings, outputBindings...)
	}
}

// WithHTTPMiddleware 向runtime添加中间件
func WithHTTPMiddleware(httpMiddleware ...http.Middleware) Option {
	return func(o *runtimeOpts) {
		o.httpMiddleware = append(o.httpMiddleware, httpMiddleware...)
	}
}

// WithComponentsCallback 为嵌入Dapr的应用程序设置组件回调。  没有找到调用的地方
func WithComponentsCallback(componentsCallback ComponentsCallback) Option {
	return func(o *runtimeOpts) {
		//func(components ComponentRegistry) error
		// 会把当前所有注册的组件传递进去
		o.componentsCallback = componentsCallback
	}
}
