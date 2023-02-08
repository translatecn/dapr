package runtime

import (
	nr "github.com/dapr/components-contrib/nameresolution"
	"github.com/dapr/dapr/pkg/components"
	"github.com/dapr/dapr/pkg/modes"
	"github.com/pkg/errors"
	"os"
	"strconv"
)

func (a *DaprRuntime) initNameResolution() error {
	var resolver nr.Resolver
	var err error
	resolverMetadata := nr.Metadata{}

	resolverName := a.globalConfig.Spec.NameResolutionSpec.Component
	resolverVersion := a.globalConfig.Spec.NameResolutionSpec.Version

	if resolverName == "" {
		switch a.runtimeConfig.Mode {
		case modes.KubernetesMode:
			resolverName = "kubernetes"
		case modes.StandaloneMode:
			resolverName = "mdns"
		default:
			return errors.Errorf("无法确定域名解析 %s mode", string(a.runtimeConfig.Mode))
		}
	}

	if resolverVersion == "" {
		resolverVersion = components.FirstStableVersion
	}

	resolver, err = a.nameResolutionRegistry.Create(resolverName, resolverVersion) // 只创建结构
	resolverMetadata.Configuration = a.globalConfig.Spec.NameResolutionSpec.Configuration
	resolverMetadata.Properties = map[string]string{
		nr.DaprHTTPPort: strconv.Itoa(a.runtimeConfig.HTTPPort),         // 3500
		nr.DaprPort:     strconv.Itoa(a.runtimeConfig.InternalGRPCPort), // 50002
		nr.AppPort:      strconv.Itoa(a.runtimeConfig.ApplicationPort),  //3001
		nr.HostAddress:  a.hostAddress,                                  // 10.10.16.115
		nr.AppID:        a.runtimeConfig.ID,                             // dp-618b5e4aa5ebc3924db86860-executorapp
		//改变其他nr组件以使用上述属性（特别是MDNS组件）。
		nr.MDNSInstanceName:    a.runtimeConfig.ID,                             // dp-618b5e4aa5ebc3924db86860-executorapp
		nr.MDNSInstanceAddress: a.hostAddress,                                  // 10.10.16.115
		nr.MDNSInstancePort:    strconv.Itoa(a.runtimeConfig.InternalGRPCPort), // 3500
	}

	if err != nil {
		log.Warnf("创建域名解析实例失败 %s: %s", resolverName, err)
		return err
	}
	// 只是更改clusterDomain属性
	if err = resolver.Init(resolverMetadata); err != nil {
		log.Errorf("创建域名解析实例失败 %s: %s", resolverName, err)
		return err
	}

	a.nameResolver = resolver

	log.Infof("已初始化域名解析实例%s", resolverName)
	return nil
}
func (a *DaprRuntime) getNamespace() string {
	return os.Getenv("NAMESPACE")
}
