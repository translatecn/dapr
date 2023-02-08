<div style="text-align: center"><img src="/img/dapr_logo.svg" height="120px">
 
</div>

[![Go Report Card](https://goreportcard.com/badge/github.com/dapr/dapr)](https://goreportcard.com/report/github.com/dapr/dapr)
[![Docker Pulls](https://img.shields.io/docker/pulls/daprio/daprd)](https://hub.docker.com/r/daprio/dapr)
[![Build Status](https://github.com/dapr/dapr/workflows/dapr/badge.svg?event=push&branch=master)](https://github.com/dapr/dapr/actions?workflow=dapr)
[![Scheduled e2e test](https://github.com/dapr/dapr/workflows/dapr-test/badge.svg?event=schedule)](https://github.com/dapr/dapr/actions?workflow=dapr-test)
[![codecov](https://codecov.io/gh/dapr/dapr/branch/master/graph/badge.svg)](https://codecov.io/gh/dapr/dapr)
[![Discord](https://img.shields.io/discord/778680217417809931)](https://discord.com/channels/778680217417809931/778680217417809934)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![TODOs](https://badgen.net/https/api.tickgit.com/badgen/github.com/dapr/dapr)](https://www.tickgit.com/browse?repo=github.com/dapr/dapr)
[![Follow on Twitter](https://img.shields.io/twitter/follow/daprdev.svg?style=social&logo=twitter)](https://twitter.com/intent/follow?screen_name=daprdev)

Dapr是一种可移植的、无服务器的、事件驱动的运行时，它使开发人员能够轻松地构建运行在云和边缘上的有弹性的、无状态的和有状态的微服务，并拥抱语言和开发人员框架的多样性。


中文注释

## Code of Conduct

Please refer to our [Dapr Community Code of Conduct](https://github.com/dapr/community/blob/master/CODE-OF-CONDUCT.md)

operator = controlPlaneAddress = dapr-api.dapr-system.svc.cluster.local:80


- daprd sidecar负责流量代理、etc
  - 启动过程中会拿着从环境变量获取到ca,cert,key 与 operator 建立grpc连接 ;
  - 会与sentry 通信, 证书签名
  - 会与operator通信，加载组件配置[crd]；去获取dapr的全局配置
  - 会与placement通信，服务注册、发现
  - 会在启动时进行订阅  pkg/runtime/runtime.go:535
  - 域名解析 只在 initDirectMessaging 有用 【服务调用】
  - dapr actor 会将自身信息注册到placement,同时placement会将相同信息广播到相同的actor实例上
  - Dapr actor Placement 服务仅用于 actor 放置，因此如果您的服务不使用 Dapr actor，则不需要。 
  - Placement 服务可以运行在所有 ，包括自托管和 Kubernetes。
- 单机模式下，去加目录下获取组件
- injector 负责将daprd注入到副本集中
- placement  raft 只负责记录actor 实例 ;以及将所有actor记录同步到每一个actor实例中
  - backoff.Retry 函数重试
  - 1、在同步actor数据到client时,为什么要分三次请求，lock,update,unlock
  - 2、placement收到【客户端流】client心跳时,并不会等到raft同步完后再返回，而是交给了一个channel处理
    - pkg/placement/placement.go:234

``` 
    -   /usr/local/go/src/crypto/x509/root_unix.go:20
        SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
```

```
daprd 在与远程服务进行通信时，会为每个服务保持一个链接
pkg/grpc/grpc.go:79
sidecar 之间是通过grpc链接的
```

### todo
- 1、operator 自定义crd ; 使用kubebuilder
