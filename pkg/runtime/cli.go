// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package runtime

import (
	"flag"
	"fmt"
	daprd_debug "github.com/dapr/dapr/code_debug/daprd"
	"github.com/dapr/dapr/pkg/operator/client"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/pkg/errors"

	"github.com/dapr/kit/logger" // ok

	"github.com/dapr/dapr/pkg/acl"
	global_config "github.com/dapr/dapr/pkg/config"
	env "github.com/dapr/dapr/pkg/config/env"
	"github.com/dapr/dapr/pkg/cors"
	"github.com/dapr/dapr/pkg/grpc"
	"github.com/dapr/dapr/pkg/metrics"
	"github.com/dapr/dapr/pkg/modes"
	"github.com/dapr/dapr/pkg/runtime/security"
	"github.com/dapr/dapr/pkg/version" // ok
	"github.com/dapr/dapr/utils"       // ok
)

// FromFlags 解析命令行参数, 返回DaprRuntime 实例
func FromFlags() (*DaprRuntime, error) {
	mode := flag.String("mode", string(modes.StandaloneMode), "Runtime mode for Dapr")
	daprHTTPPort := flag.String("dapr-http-port", fmt.Sprintf("%v", DefaultDaprHTTPPort), "HTTP port for Dapr API to listen on")
	daprAPIListenAddresses := flag.String("dapr-listen-addresses", DefaultAPIListenAddress, "One or more addresses for the Dapr API to listen on, CSV limited")
	// 健康检查、元数据 监听端口
	daprPublicPort := flag.String("dapr-public-port", "", "Public port for Dapr Health and Metadata to listen on")
	daprAPIGRPCPort := flag.String("dapr-grpc-port", fmt.Sprintf("%v", DefaultDaprAPIGRPCPort), "gRPC port for the Dapr API to listen on")
	daprInternalGRPCPort := flag.String("dapr-internal-grpc-port", "", "gRPC port for the Dapr Internal API to listen on")

	appPort := flag.String("app-port", "", "The port the application is listening on")
	profilePort := flag.String("profile-port", fmt.Sprintf("%v", DefaultProfilePort), "The port for the profile server")
	appProtocol := flag.String("app-protocol", string(HTTPProtocol), "Protocol for the application: grpc or http")
	componentsPath := flag.String("components-path", "~/.dapr/components", "Path for components directory. If empty, components will not be loaded. Self-hosted mode only")
	config := flag.String("config", "", "Path to config file, or name of a configuration object")
	appID := flag.String("app-id", "", "A unique ID for Dapr. Used for Service Discovery and state")
	controlPlaneAddress := flag.String("control-plane-address", "", "Address for a Dapr control plane")
	sentryAddress := flag.String("sentry-address", "", "Address for the Sentry CA service")
	placementServiceHostAddr := flag.String("placement-host-address", "", "Addresses for Dapr Actor Placement servers")
	allowedOrigins := flag.String("allowed-origins", cors.DefaultAllowedOrigins, "Allowed HTTP origins")
	enableProfiling := flag.Bool("enable-profiling", false, "Enable profiling")
	runtimeVersion := flag.Bool("version", false, "Prints the runtime version")
	buildInfo := flag.Bool("build-info", false, "Prints the build info")
	//等待Dapr出站准备就绪
	waitCommand := flag.Bool("wait", false, "wait for Dapr outbound ready")
	appMaxConcurrency := flag.Int("app-max-concurrency", -1, "Controls the concurrency level when forwarding requests to user code")
	enableMTLS := flag.Bool("enable-mtls", false, "Enables automatic mTLS for daprd to daprd communication channels")
	//将应用程序的URI方案设置为https并尝试SSL连接
	appSSL := flag.Bool("app-ssl", false, "Sets the URI scheme of the app to https and attempts an SSL connection")
	// 请求体的最大大小(以MB为单位)来处理大文件的上传。默认为4mb。
	daprHTTPMaxRequestSize := flag.Int("dapr-http-max-request-size", -1, "Increasing max size of request body in MB to handle uploading of big files. By default 4 MB.")
	//到unix域套接字目录的路径。如果指定，Dapr API服务器将使用Unix域套接字
	unixDomainSocket := flag.String("unix-domain-socket", "", "Path to a unix domain socket dir mount. If specified, Dapr API servers will use Unix Domain Sockets")
	//增加以KB为单位的读缓冲区的最大大小，以处理发送多KB的报头。缺省值为4kb。
	daprHTTPReadBufferSize := flag.Int("dapr-http-read-buffer-size", -1, "Increasing max size of read buffer in KB to handle sending multi-KB headers. By default 4 KB.")
	//在http服务器上启用请求正文流
	daprHTTPStreamRequestBody := flag.Bool("dapr-http-stream-request-body", false, "Enables request body streaming on http server")
	loggerOptions := logger.DefaultOptions()
	loggerOptions.AttachCmdFlags(flag.StringVar, flag.BoolVar)

	metricsExporter := metrics.NewExporter(metrics.DefaultMetricNamespace)

	metricsExporter.Options().AttachCmdFlags(flag.StringVar, flag.BoolVar)
	daprd_debug.PRE(
		// *string
		mode, daprHTTPPort, daprAPIGRPCPort,
		appPort, appID, controlPlaneAddress, appProtocol, placementServiceHostAddr, config,
		sentryAddress, &loggerOptions.OutputLevel,
		&metricsExporter.Options().Port,
		daprInternalGRPCPort,
		//*int
		appMaxConcurrency,
		daprHTTPMaxRequestSize,
		//*bool
		&metricsExporter.Options().MetricsEnabled,
		enableMTLS,
	)

	flag.Parse()
	loggerOptions.SetOutputLevel("debug")
	if *runtimeVersion {
		fmt.Println(version.Version())
		os.Exit(0)
	}

	if *buildInfo {
		fmt.Printf("Version: %s\nGit Commit: %s\nGit Version: %s\n", version.Version(), version.Commit(), version.GitVersion())
		os.Exit(0)
	}

	if *waitCommand {
		//等待，直到Dapr 输出绑定 就绪
		waitUntilDaprOutboundReady(*daprHTTPPort)
		os.Exit(0)
	}

	if *appID == "" {
		return nil, errors.New("app-id parameter cannot be empty")
	}

	// 设置应用ID
	loggerOptions.SetAppID(*appID)
	//将配置应用到所有注册的logger上 ，都在全局变量初始化好了

	if err := logger.ApplyOptionsToLoggers(&loggerOptions); err != nil {
		return nil, err
	}

	log.Infof("starting Dapr Runtime -- version %s -- commit %s", version.Version(), version.Commit())
	log.Infof("log level set to: %s", loggerOptions.OutputLevel)

	// 启动指标暴露程序  9090端口
	if err := metricsExporter.Init(); err != nil {
		log.Fatal(err)
	}
	//dapr-http: 3500/TCP
	//dapr-grpc: 50001/TCP
	//dapr-internal: 50002/TCP
	//dapr-metrics: 9090/TCP
	//requests.get("http://localhost:9090").text 获取指标

	// http   ------>    daprd 3500 <----->  app appPort
	// grpc  ------->   50001 daprd 50002 <-------> app appPort

	daprHTTP, err := strconv.Atoi(*daprHTTPPort)
	if err != nil {
		return nil, errors.Wrap(err, "error parsing dapr-http-port flag")
	}

	daprAPIGRPC, err := strconv.Atoi(*daprAPIGRPCPort)
	if err != nil {
		return nil, errors.Wrap(err, "error parsing dapr-grpc-port flag")
	}

	// 默认7777
	profPort, err := strconv.Atoi(*profilePort)
	if err != nil {
		return nil, errors.Wrap(err, "error parsing profile-port flag")
	}

	var daprInternalGRPC int
	if *daprInternalGRPCPort != "" {
		daprInternalGRPC, err = strconv.Atoi(*daprInternalGRPCPort)
		if err != nil {
			return nil, errors.Wrap(err, "error parsing dapr-internal-grpc-port")
		}
	} else {
		//返回一个来自操作系统的空闲端口。
		daprInternalGRPC, err = grpc.GetFreePort()
		if err != nil {
			return nil, errors.Wrap(err, "failed to get free port for internal grpc server")
		}
	}

	var publicPort *int
	if *daprPublicPort != "" {
		port, cerr := strconv.Atoi(*daprPublicPort)
		if cerr != nil {
			return nil, errors.Wrap(cerr, "error parsing dapr-public-port")
		}
		publicPort = &port
	}

	var applicationPort int
	if *appPort != "" {
		//appPort
		applicationPort, err = strconv.Atoi(*appPort)
		if err != nil {
			return nil, errors.Wrap(err, "error parsing app-port")
		}
	}

	var maxRequestBodySize int
	if *daprHTTPMaxRequestSize != -1 {
		maxRequestBodySize = *daprHTTPMaxRequestSize
	} else {
		maxRequestBodySize = DefaultMaxRequestBodySize
	}

	var readBufferSize int
	if *daprHTTPReadBufferSize != -1 {
		readBufferSize = *daprHTTPReadBufferSize
	} else {
		readBufferSize = DefaultReadBufferSize
	}

	//
	placementAddresses := []string{}
	if *placementServiceHostAddr != "" {
		// dapr-placement-server.dapr.svc.cluster.local:50005
		placementAddresses = parsePlacementAddr(*placementServiceHostAddr)
	}

	var concurrency int
	if *appMaxConcurrency != -1 {
		concurrency = *appMaxConcurrency
	}

	appPrtcl := string(HTTPProtocol)
	if *appProtocol != string(HTTPProtocol) {
		appPrtcl = *appProtocol
	}
	// [::1],127.0.0.1
	daprAPIListenAddressList := strings.Split(*daprAPIListenAddresses, ",")
	if len(daprAPIListenAddressList) == 0 {
		daprAPIListenAddressList = []string{DefaultAPIListenAddress}
	}
	runtimeConfig := NewRuntimeConfig(*appID, placementAddresses, *controlPlaneAddress,
		*allowedOrigins, *config, *componentsPath,
		appPrtcl, *mode, daprHTTP, daprInternalGRPC, daprAPIGRPC, daprAPIListenAddressList,
		publicPort, applicationPort, profPort, *enableProfiling, concurrency, *enableMTLS,
		*sentryAddress, *appSSL, maxRequestBodySize, *unixDomainSocket, readBufferSize,
		*daprHTTPStreamRequestBody)

	// 设置环境变量
	// TODO - 考虑在运行时配置中添加主机地址或在实用程序包中缓存结果
	host, err := utils.GetHostAddress()
	if err != nil {
		log.Warnf("failed to get host address, env variable %s will not be set", env.HostAddress)
	}

	// 变量存储
	variables := map[string]string{
		env.AppID:           *appID,
		env.AppPort:         *appPort,
		env.HostAddress:     host,
		env.DaprPort:        strconv.Itoa(daprInternalGRPC),
		env.DaprGRPCPort:    *daprAPIGRPCPort,
		env.DaprHTTPPort:    *daprHTTPPort,
		env.DaprMetricsPort: metricsExporter.Options().Port, // TODO - consider adding to runtime config
		env.DaprProfilePort: *profilePort,
	}

	if err = setEnvVariables(variables); err != nil {
		return nil, err
	}

	var globalConfig *global_config.Configuration
	var configErr error

	// daprd之间是否加密通信，enableMTLS 默认false
	// k8s模式一定要加密
	if *enableMTLS || *mode == string(modes.KubernetesMode) {
		//  从环境变量中 获取根证书、证书、私钥
		runtimeConfig.CertChain, err = security.GetCertChain()
		if err != nil {
			return nil, err
		}
	}
	//访问控制列表
	var accessControlList *global_config.AccessControlList
	var namespace string

	if *config != "" {
		switch modes.DaprMode(*mode) {
		case modes.KubernetesMode:
			//controlPlaneAddress=dapr-api.dapr-system.svc.cluster.local:80

			// 建立一个 控制面的链接   链接的是
			// 将这个服务的80端口 映射到本地6500端口
			// kubectl port-forward svc/dapr-api -n dapr-system 6500:80 &
			client, conn, clientErr := client.GetOperatorClient(*controlPlaneAddress,
				security.TLSServerName, runtimeConfig.CertChain)
			if clientErr != nil {
				return nil, clientErr
			}
			defer conn.Close()
			namespace = os.Getenv("NAMESPACE")
			//去拿k8s里dapr自定义资源Configuration  appconfig
			globalConfig, configErr = global_config.LoadKubernetesConfiguration(*config, namespace, client)
		case modes.StandaloneMode:
			userHome, err := daprd_debug.Home()
			if err != nil {
				log.Fatal(err)
			}
			globalConfig, _, configErr = global_config.LoadStandaloneConfiguration(path.Join(userHome, ".dapr/config.yaml"))
		}
	}

	if configErr != nil {
		log.Fatalf("加载配置出错: %s", configErr)
	}
	if globalConfig == nil {
		log.Info("loading default configuration")
		globalConfig = global_config.LoadDefaultConfiguration()
	}

	log.Info("loading daprd accessControlList")
	accessControlList, err = acl.ParseAccessControlSpec(globalConfig.Spec.AccessControlSpec, string(runtimeConfig.ApplicationProtocol))
	if err != nil {
		log.Fatalf(err.Error())
	}
	return NewDaprRuntime(runtimeConfig, globalConfig, accessControlList), nil
}

// ok
func setEnvVariables(variables map[string]string) error {
	for key, value := range variables {
		err := os.Setenv(key, value)
		if err != nil {
			return err
		}
	}
	return nil
}

// ok
func parsePlacementAddr(val string) []string {
	// dapr-placement-server.dapr.svc.cluster.local:50005
	parsed := []string{}
	p := strings.Split(val, ",")
	for _, addr := range p {
		parsed = append(parsed, strings.TrimSpace(addr))
	}
	return parsed
}
