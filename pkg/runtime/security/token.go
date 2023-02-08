package security

import (
	"os"
	"strings"
)

/* #nosec. */
const (
	// APITokenEnvVar api token多对应的环境变量
	APITokenEnvVar    = "DAPR_API_TOKEN"
	AppAPITokenEnvVar = "APP_API_TOKEN"
	// APITokenHeader http\grpc 调用所携带的header
	APITokenHeader = "dapr-api-token"
)

var excludedRoutes = []string{"/healthz"}

// GetAPIToken 返回从环境变量中读取的 api token
func GetAPIToken() string {
	return os.Getenv(APITokenEnvVar)
}

// GetAppToken 返回环境变量中的app api token的值。
func GetAppToken() string {
	return os.Getenv(AppAPITokenEnvVar)
}

// ExcludedRoute 返回一个给定的路由是否应该被排除在标记检查之外。
func ExcludedRoute(route string) bool {
	for _, r := range excludedRoutes {
		if strings.Contains(route, r) {
			return true
		}
	}
	return false
}
