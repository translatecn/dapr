// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package main

import (
	"flag"
	injector_debug "github.com/dapr/dapr/code_debug/injector"
	scheme "github.com/dapr/dapr/pkg/client/clientset/versioned"
	"os"
	"time"

	"github.com/dapr/dapr/pkg/health" //ok
	"github.com/dapr/dapr/pkg/injector"
	"github.com/dapr/dapr/pkg/injector/monitoring" // ok
	"github.com/dapr/dapr/pkg/metrics"             // ok
	"github.com/dapr/dapr/pkg/signals"             //ok
	"github.com/dapr/dapr/pkg/version"             //ok
	"github.com/dapr/dapr/utils"                   // ok
	"github.com/dapr/kit/logger"                   // ok
)

// LIKE
var log = logger.NewLogger("dapr.injector")

const (
	healthzPort = 8080
)

func main() {
	logger.DaprVersion = version.Version()
	log.Info(os.Getpid())
	log.Infof("starting Dapr Sidecar Injector -- version %s -- commit %s", version.Version(), version.Commit())

	// 处理程序接收kill 信号
	ctx := signals.Context()
	injector_debug.PRE()

	// k8s client
	kubeClient := utils.GetKubeClient()
	// k8s conf
	conf := utils.GetConfig()
	daprClient, _ := scheme.NewForConfig(conf)
	// 判断本服务是不是活着
	go func() {
		healthzServer := health.NewServer(log)
		healthzServer.Ready()

		healthzErr := healthzServer.Run(ctx, healthzPort)
		if healthzErr != nil {
			log.Fatalf("failed to start healthz server: %s", healthzErr)
		}
	}()
	// 获取kube-system名称空间下各副本控制器的UUID【唯一标识】
	uids, err := injector.AllowedControllersServiceAccountUID(ctx, kubeClient)
	if err != nil {
		log.Fatalf("failed to get authentication uids from services accounts: %s", err)
	}
	// 主要是获取TLS文件以
	cfg, err := injector.GetConfig()
	if err != nil {
		log.Fatalf("error getting config: %s", err)
	}
	//debug
	_, _, _ = injector.GetTrustAnchorsAndCertChain(kubeClient, "dapr")

	// in order to debug
	injector.NewInjector(uids, cfg, daprClient, kubeClient).Run(ctx)
	// 默认是不会走到此处的，
	// 走到此处，说明注入程序启动失败

	shutdownDuration := 5 * time.Second
	log.Infof("allowing %s for graceful shutdown to complete", shutdownDuration)
	<-time.After(shutdownDuration)
}

// 8080 /healthz
// 4000 注入
// 9090 运行指标暴露

//ENV
//KUBE_CLUSTER_DOMAIN: cluster.local
//NAMESPACE: fieldRef(v1:metadata.namespace)
//SIDECAR_IMAGE: docker.io/daprio/daprd:1.3.0
//SIDECAR_IMAGE_PULL_POLICY: IfNotPresent
//TLS_CERT_FILE: /dapr/cert/tls.crt
//TLS_KEY_FILE: /dapr/cert/tls.key
func init() {
	loggerOptions := logger.DefaultOptions()
	// 这样里面的包不用依赖 flag
	loggerOptions.AttachCmdFlags(flag.StringVar, flag.BoolVar)

	// 创建普罗米修斯的指标exporter
	metricsExporter := metrics.NewExporter(metrics.DefaultMetricNamespace)
	metricsExporter.Options().AttachCmdFlags(flag.StringVar, flag.BoolVar)

	flag.Parse()

	// 对全局的所有logger进行了设置
	if err := logger.ApplyOptionsToLoggers(&loggerOptions); err != nil {
		log.Fatal(err)
	} else {
		log.Infof("log level set to: %s", loggerOptions.OutputLevel)
	}

	// 初始化dapr指标输出器
	if err := metricsExporter.Init(); err != nil {
		log.Fatal(err)
	}

	// 初始化注入器服务指标
	if err := monitoring.InitMetrics(); err != nil {
		log.Fatal(err)
	}
}
