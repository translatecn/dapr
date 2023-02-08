package daprd_debug

import (
	"github.com/dapr/dapr/pkg/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"time"
)

func main() {
	_time, err := time.Parse("", "2021-11-24T11:26:11Z")
	if err != nil {
		return
	}
	_ = config.Configuration{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Configuration",
			APIVersion: "dapr.io/va.alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:                       "appconfig",
			GenerateName:               "",
			Namespace:                  "mesoid",
			SelfLink:                   "",
			UID:                        types.UID("f0d2dc48-df19-45b5-8157-189e05fd10d1"),
			ResourceVersion:            "20260084",
			Generation:                 1,
			CreationTimestamp:          metav1.Time{
				Time: _time,
			},
			DeletionTimestamp:          nil,
			DeletionGracePeriodSeconds: nil,
			Labels:                     nil,
			Annotations:                nil,
			OwnerReferences:            nil,
			Finalizers:                 nil,
			ClusterName:                "",
			ManagedFields:              nil,
		},
		Spec: config.ConfigurationSpec{
			HTTPPipelineSpec: config.PipelineSpec{
				Handlers: nil,
			},
			TracingSpec: config.TracingSpec{
				SamplingRate: "1",
				Stdout:       false,
				Zipkin: config.ZipkinSpec{
					EndpointAddress: "http://zipkin.mesoid.svc.cluster.local:9411/api/v2/span",
				},
			},
			MTLSSpec: config.MTLSSpec{
				Enabled:          false,
				WorkloadCertTTL:  "",
				AllowedClockSkew: "",
			},
			MetricSpec: config.MetricSpec{
				Enabled: true,
			},
			Secrets: config.SecretsSpec{
				Scopes: []config.SecretsScope{},
			},
			AccessControlSpec: config.AccessControlSpec{
				DefaultAction: "",
				TrustDomain:   "",
				AppPolicies:   nil,
			},
			// 会根据这里进行名称解析组件的初始化
			NameResolutionSpec: config.NameResolutionSpec{
				Component:     "",
				Version:       "",
				Configuration: nil,
			},
			Features: []config.FeatureSpec{},
			APISpec:  config.APISpec{},
		},
	}
}

//Configuration
