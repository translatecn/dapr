package runtime

import (
	nr "github.com/dapr/components-contrib/nameresolution"
	"github.com/dapr/dapr/pkg/messaging"
)

func (a *DaprRuntime) initDirectMessaging(resolver nr.Resolver) {
	a.directMessaging = messaging.NewDirectMessaging(
		a.runtimeConfig.ID,
		a.namespace,
		a.runtimeConfig.InternalGRPCPort,
		a.runtimeConfig.Mode,
		a.appChannel,
		a.grpc.GetGRPCConnection,
		resolver,
		a.globalConfig.Spec.TracingSpec,
		a.runtimeConfig.MaxRequestBodySize,
		a.proxy,
		a.runtimeConfig.ReadBufferSize,
		a.runtimeConfig.StreamRequestBody,
	)
}
