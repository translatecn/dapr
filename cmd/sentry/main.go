// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package main

import (
	"flag"
	sentry_debug "github.com/dapr/dapr/code_debug/sentry"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dapr/kit/logger" //ok

	"github.com/dapr/dapr/pkg/credentials" // ok
	"github.com/dapr/dapr/pkg/fswatcher"   // ok 获取证书文件
	"github.com/dapr/dapr/pkg/health"      // ok
	"github.com/dapr/dapr/pkg/metrics"     // ok
	// 都没有init方法
	"github.com/dapr/dapr/pkg/sentry" // ok
	"github.com/dapr/dapr/pkg/sentry/config"
	"github.com/dapr/dapr/pkg/sentry/monitoring" // ok
	"github.com/dapr/dapr/pkg/signals"           // ok
	"github.com/dapr/dapr/pkg/version"           // ok
	_ "net/http/pprof"
)

var log = logger.NewLogger("dapr.sentry")

const (
	defaultCredentialsPath = "/var/run/dapr/credentials"
	// 自定义资源
	// defaultDaprSystemConfigName 是Dapr System Config的默认资源对象名称。
	defaultDaprSystemConfigName = "daprsystem"

	healthzPort = 8080
)

func main() {
	go func() {
		err := http.ListenAndServe("127.0.0.1:9999", nil)
		if err != nil {
			log.Fatal(err)
		}
	}()
	err := os.Setenv("KUBERNETES_SERVICE_HOST", "1123213")
	if err != nil {
		log.Error(err)
	}
	configName := flag.String("config", defaultDaprSystemConfigName, "Path to config file, or name of a configuration object")
	credsPath := flag.String("issuer-credentials", defaultCredentialsPath, "Path to the credentials directory holding the issuer data")
	trustDomain := flag.String("trust-domain", "localhost", "The CA trust domain")

	loggerOptions := logger.DefaultOptions()
	loggerOptions.AttachCmdFlags(flag.StringVar, flag.BoolVar)

	metricsExporter := metrics.NewExporter(metrics.DefaultMetricNamespace)
	metricsExporter.Options().AttachCmdFlags(flag.StringVar, flag.BoolVar)

	flag.Parse()
	loggerOptions.SetOutputLevel("debug")

	// pass
	if err := logger.ApplyOptionsToLoggers(&loggerOptions); err != nil {
		log.Fatal(err)
	}

	log.Infof("starting sentry certificate authority -- version %s -- commit %s", version.Version(), version.Commit())
	log.Infof("log level set to: %s", loggerOptions.OutputLevel)

	// pass
	if err := metricsExporter.Init(); err != nil {

		log.Fatal(err)
	}
	// pass
	if err := monitoring.InitMetrics(); err != nil {
		log.Fatal(err)
	}

	issuerCertPath := filepath.Join(*credsPath, credentials.IssuerCertFilename)
	issuerKeyPath := filepath.Join(*credsPath, credentials.IssuerKeyFilename)
	rootCertPath := filepath.Join(*credsPath, credentials.RootCertFilename)
	//_, _ = config.GetDefaultConfig(*configName)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	ctx := signals.Context()
	config, err := config.FromConfigName(*configName) // 会定义 kubeconfig 的环境变量 与  ca.Run 【更改获取k8s客户端】 会重复定义
	if err != nil {
		log.Warn(err)
	}
	config.IssuerCertPath = issuerCertPath
	config.IssuerKeyPath = issuerKeyPath
	config.RootCertPath = rootCertPath
	config.TrustDomain = *trustDomain
	sentry_debug.PRE(&config)
	watchDir := filepath.Dir(config.IssuerCertPath)

	ca := sentry.NewSentryCA()

	log.Infof("starting watch on filesystem directory: %s", watchDir)
	issuerEvent := make(chan struct{})
	ready := make(chan bool)

	// 启动CA服务 grpc通信
	go ca.Run(ctx, config, ready)

	<-ready
	go fswatcher.Watch(ctx, watchDir, issuerEvent)

	go func() {
		for range issuerEvent {
			// 指标变更
			monitoring.IssuerCertChanged()
			log.Warn("发行人证书已更改。 重新加载")
			ca.Restart(ctx, config)
		}
	}()

	go func() {
		healthzServer := health.NewServer(log)
		healthzServer.Ready()

		err := healthzServer.Run(ctx, healthzPort)
		if err != nil {
			log.Fatalf("failed to start healthz server: %s", err)
		}
	}()
	<-stop
	// 收到终止信号，直接等5秒 等其他goroutinue关闭
	// code_debug/sentry/signal_test.go:13
	shutdownDuration := 5 * time.Second
	log.Infof("allowing %s for graceful shutdown to complete", shutdownDuration)
	<-time.After(shutdownDuration)
}

// 8080 健康检查
// 50001
// 9090 指标采集
