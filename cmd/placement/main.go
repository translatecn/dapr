// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package main

import (
	"context"
	"github.com/dapr/dapr/pkg/placement/monitoring"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/dapr/kit/logger" //ok

	"github.com/dapr/dapr/pkg/credentials" //ok
	"github.com/dapr/dapr/pkg/fswatcher"   // ok
	"github.com/dapr/dapr/pkg/health"      // ok
	"github.com/dapr/dapr/pkg/placement"
	"github.com/dapr/dapr/pkg/placement/hashing"
	"github.com/dapr/dapr/pkg/placement/raft"
	"github.com/dapr/dapr/pkg/version" // ok
)

var log = logger.NewLogger("dapr.placement")

const gracefulTimeout = 10 * time.Second

//有状态服务
// 1. 稳定的路由
// 2. 状态
// 3. 处理是单线程
//
//api: 50005/TCP
//raft-node: 8201/TCP
//metrics: 9090/TCP
func main() {
	log.Info(os.Getpid())
	logger.DaprVersion = version.Version()
	log.Infof("starting Dapr Placement Service -- version %s -- commit %s", version.Version(), version.Commit())

	// raft、秘钥配置
	cfg := newConfig()

	// 将cfg.loggerOptions的配置设置所有logger
	if err := logger.ApplyOptionsToLoggers(&cfg.loggerOptions); err != nil {
		log.Fatal(err)
	}
	log.Infof("log level set to: %s", cfg.loggerOptions.OutputLevel)

	// 启动export,默认9090 可以获取运行指标
	if err := cfg.metricsExporter.Init(); err != nil {
		log.Fatal(err)
	}
	// 初始化配置的dapr指标
	if err := monitoring.InitMetrics(); err != nil {
		log.Fatal(err)
	}
	// http://127.0.0.1:8080/healthz 根据响应码判断运行状态
	go startHealthzServer(cfg.healthzPort)

	// 启动raft集群，
	raftServer := raft.New(cfg.raftID, cfg.raftInMemEnabled, cfg.raftPeers, cfg.raftLogStorePath)
	if raftServer == nil {
		log.Fatal("failed to create raft server.")
	}
	//cmd/placement/config.go:50
	//Raft server is starting on 127.0.0.1:8201
	if err := raftServer.StartRaft(nil); err != nil {
		log.Fatalf("failed to start Raft Server: %v", err)
	}

	// Start Placement gRPC server. // 复制因子 100
	hashing.SetReplicationFactor(cfg.replicationFactor)

	apiServer := placement.NewPlacementService(raftServer)
	var certChain *credentials.CertChain
	if cfg.tlsEnabled {
		certChain = loadCertChains(cfg.certChainPath)
	}

	go apiServer.MonitorLeadership() // 开始监听角色变换
	//50005端口, tcp 非http
	go apiServer.Run(strconv.Itoa(cfg.placementPort), certChain)
	log.Infof("placement service started on port %d", cfg.placementPort)

	// Relay incoming process signal to exit placement gracefully
	signalCh := make(chan os.Signal, 10)
	gracefulExitCh := make(chan struct{})
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signalCh)

	<-signalCh

	// Shutdown servers
	go func() {
		apiServer.Shutdown()
		raftServer.Shutdown()
		close(gracefulExitCh)
	}()

	select {
	case <-time.After(gracefulTimeout):
		log.Info("Timeout on graceful leave. Exiting...")
		os.Exit(1)

	case <-gracefulExitCh:
		log.Info("Gracefully exit.")
		os.Exit(0)
	}
}

func startHealthzServer(healthzPort int) {
	healthzServer := health.NewServer(log)
	healthzServer.Ready()

	if err := healthzServer.Run(context.Background(), healthzPort); err != nil {
		log.Fatalf("failed to start healthz server: %s", err)
	}
}

func loadCertChains(certChainPath string) *credentials.CertChain {
	tlsCreds := credentials.NewTLSCredentials(certChainPath)

	log.Info("mTLS enabled, getting tls certificates")
	// 尝试加载从硬盘证书，如果没有，则监听文件夹的变动[会阻塞]
	chain, err := credentials.LoadFromDisk(tlsCreds.RootCertPath(), tlsCreds.CertPath(), tlsCreds.KeyPath())
	if err != nil {
		fsevent := make(chan struct{})

		go func() {
			log.Infof("starting watch for certs on filesystem: %s", certChainPath)
			err = fswatcher.Watch(context.Background(), tlsCreds.Path(), fsevent)
			if err != nil {
				log.Fatal("error starting watch on filesystem: %s", err)
			}
		}()

		<-fsevent
		log.Info("发现证书")

		chain, err = credentials.LoadFromDisk(tlsCreds.RootCertPath(), tlsCreds.CertPath(), tlsCreds.KeyPath())
		if err != nil {
			log.Fatal("failed to load cert chain from disk: %s", err)
		}
	}

	log.Info("tls certificates loaded successfully")

	return chain
}

// ./placement -id dapr-placement-0 -port 30001 -initial-cluster dapr-placement-0=127.0.0.1:8201,dapr-placement-1=127.0.0.1:8202,dapr-placement-2=127.0.0.1:8203
// ./placement -id dapr-placement-1 -port 30002 -initial-cluster dapr-placement-0=127.0.0.1:8201,dapr-placement-1=127.0.0.1:8202,dapr-placement-2=127.0.0.1:8203
// ./placement -id dapr-placement-2 -port 30003 -initial-cluster dapr-placement-0=127.0.0.1:8201,dapr-placement-1=127.0.0.1:8202,dapr-placement-2=127.0.0.1:8203
