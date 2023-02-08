package config

//  pkg/runtime/config.go:19

const (
	// HostAddress 实例地址
	HostAddress string = "HOST_ADDRESS"
	// DaprGRPCPort 是dapr api grpc端口。
	DaprGRPCPort string = "DAPR_GRPC_PORT"
	// DaprHTTPPort 是dapr api http端口。
	DaprHTTPPort string = "DAPR_HTTP_PORT"
	// DaprMetricsPort   指标监控的端口
	DaprMetricsPort string = "DAPR_METRICS_PORT"
	// DaprProfilePort 性能分析的端口
	DaprProfilePort string = "DAPR_PROFILE_PORT"
	// DaprPort dpar间grpc通信的端口
	DaprPort string = "DAPR_PORT"
	// AppPort 应用端口 http、grpc
	AppPort string = "APP_PORT"
	// AppID 应用ID
	AppID string = "APP_ID"
)
