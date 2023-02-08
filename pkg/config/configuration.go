// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package config

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"

	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	"github.com/pkg/errors"
	yaml "gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	operatorv1pb "github.com/dapr/dapr/pkg/proto/operator/v1"
)

const (
	operatorCallTimeout         = time.Second * 5
	operatorMaxRetries          = 100
	AllowAccess                 = "allow"
	DenyAccess                  = "deny"
	DefaultTrustDomain          = "public"
	DefaultNamespace            = "default"
	ActionPolicyApp             = "app"
	ActionPolicyGlobal          = "global"
	SpiffeIDPrefix              = "spiffe://"
	HTTPProtocol                = "http"
	GRPCProtocol                = "grpc"
	ActorReentrancy     Feature = "Actor.Reentrancy" // actor重入
	ActorTypeMetadata   Feature = "Actor.TypeMetadata"
	PubSubRouting       Feature = "PubSub.Routing"
	StateEncryption     Feature = "State.Encryption"
)

type Feature string

// Configuration 是Dapr的配置CRD的内部（和重复）表示。
type Configuration struct {
	metav1.TypeMeta `json:",inline" yaml:",inline"`
	// See https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	// See https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Spec ConfigurationSpec `json:"spec" yaml:"spec"`
}

// AccessControlList 是用于快速查找的内存访问控制列表配置。
type AccessControlList struct {
	DefaultAction string //
	TrustDomain   string //
	PolicySpec    map[string]AccessControlListPolicySpec
}

// AccessControlListPolicySpec 是每个应用程序的内存访问控制列表配置，用于快速查找。
type AccessControlListPolicySpec struct {
	AppName             string // 应用名字
	DefaultAction       string
	TrustDomain         string
	Namespace           string
	AppOperationActions map[string]AccessControlListOperationAction // 应用操作行为
}

// AccessControlListOperationAction 是每个操作的内存访问控制列表配置，用于快速查找。
type AccessControlListOperationAction struct {
	VerbAction       map[string]string // 其实就是http method
	OperationPostFix string
	OperationAction  string
}

type ConfigurationSpec struct {
	HTTPPipelineSpec   PipelineSpec       `json:"httpPipeline,omitempty" yaml:"httpPipeline,omitempty"`
	TracingSpec        TracingSpec        `json:"tracing,omitempty" yaml:"tracing,omitempty"`
	MTLSSpec           MTLSSpec           `json:"mtls,omitempty" yaml:"mtls,omitempty"`
	MetricSpec         MetricSpec         `json:"metric,omitempty" yaml:"metric,omitempty"`
	Secrets            SecretsSpec        `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	AccessControlSpec  AccessControlSpec  `json:"accessControl,omitempty" yaml:"accessControl,omitempty"`
	NameResolutionSpec NameResolutionSpec `json:"nameResolution,omitempty" yaml:"nameResolution,omitempty"`
	Features           []FeatureSpec      `json:"features,omitempty" yaml:"features,omitempty"`
	APISpec            APISpec            `json:"api,omitempty" yaml:"api,omitempty"`
}

type SecretsSpec struct {
	Scopes []SecretsScope `json:"scopes"`
}

// SecretsScope   定义了secret的范围。
type SecretsScope struct {
	DefaultAccess  string   `json:"defaultAccess,omitempty" yaml:"defaultAccess,omitempty"`
	StoreName      string   `json:"storeName" yaml:"storeName"`
	AllowedSecrets []string `json:"allowedSecrets,omitempty" yaml:"allowedSecrets,omitempty"`
	DeniedSecrets  []string `json:"deniedSecrets,omitempty" yaml:"deniedSecrets,omitempty"`
}

type PipelineSpec struct {
	Handlers []HandlerSpec `json:"handlers" yaml:"handlers"`
}

// APISpec Dapr API的配置信息
type APISpec struct {
	Allowed []APIAccessRule `json:"allowed,omitempty"`
}

// APIAccessRule 描述了一个允许应用程序启用和访问Dapr API的访问规则。
type APIAccessRule struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Protocol string `json:"protocol"`
}

type HandlerSpec struct {
	Name         string       `json:"name" yaml:"name"`
	Type         string       `json:"type" yaml:"type"`
	Version      string       `json:"version" yaml:"version"`
	SelectorSpec SelectorSpec `json:"selector,omitempty" yaml:"selector,omitempty"`
}

type SelectorSpec struct {
	Fields []SelectorField `json:"fields" yaml:"fields"`
}

type SelectorField struct {
	Field string `json:"field" yaml:"field"`
	Value string `json:"value" yaml:"value"`
}

type TracingSpec struct {
	SamplingRate string     `json:"samplingRate" yaml:"samplingRate"`
	Stdout       bool       `json:"stdout" yaml:"stdout"`
	Zipkin       ZipkinSpec `json:"zipkin" yaml:"zipkin"`
}

// ZipkinSpec 定义了zipkin的配置
type ZipkinSpec struct {
	EndpointAddress string `json:"endpointAddress" yaml:"endpointAddress"`
}

// MetricSpec 监控是否开启
type MetricSpec struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// AppPolicySpec 为每个应用程序定义策略数据结构。
type AppPolicySpec struct {
	AppName             string         `json:"appId" yaml:"appId"`
	DefaultAction       string         `json:"defaultAction" yaml:"defaultAction"`
	TrustDomain         string         `json:"trustDomain" yaml:"trustDomain"`
	Namespace           string         `json:"namespace" yaml:"namespace"`
	AppOperationActions []AppOperation `json:"operations" yaml:"operations"`
}

// AppOperation 定义每个应用程序操作的数据结构。
type AppOperation struct {
	Operation string   `json:"name" yaml:"name"`
	HTTPVerb  []string `json:"httpVerb" yaml:"httpVerb"`
	Action    string   `json:"action" yaml:"action"`
}

// AccessControlSpec 是ConfigurationSpec中的spec对象。
type AccessControlSpec struct {
	DefaultAction string          `json:"defaultAction" yaml:"defaultAction"`
	TrustDomain   string          `json:"trustDomain" yaml:"trustDomain"`
	AppPolicies   []AppPolicySpec `json:"policies" yaml:"policies"`
}

type NameResolutionSpec struct {
	Component     string      `json:"component" yaml:"component"`
	Version       string      `json:"version" yaml:"version"`
	Configuration interface{} `json:"configuration" yaml:"configuration"`
}

type MTLSSpec struct {
	Enabled          bool   `json:"enabled" yaml:"enabled"`
	WorkloadCertTTL  string `json:"workloadCertTTL" yaml:"workloadCertTTL"`
	AllowedClockSkew string `json:"allowedClockSkew" yaml:"allowedClockSkew"`
}

// SpiffeID 表示spiffe id中分隔的字段。
type SpiffeID struct {
	TrustDomain string
	Namespace   string
	AppID       string
}

// FeatureSpec 定义启用哪些预览功能。
type FeatureSpec struct {
	Name    Feature `json:"name" yaml:"name"`
	Enabled bool    `json:"enabled" yaml:"enabled"`
}

// LoadDefaultConfiguration 返回默认配置。
func LoadDefaultConfiguration() *Configuration {
	return &Configuration{
		Spec: ConfigurationSpec{
			TracingSpec: TracingSpec{ // 追踪
				SamplingRate: "",
			},
			MetricSpec: MetricSpec{ // 指标监控
				Enabled: true,
			},
			AccessControlSpec: AccessControlSpec{ //访问控制
				DefaultAction: AllowAccess,
				TrustDomain:   "public",
			},
		},
	}
}

// LoadStandaloneConfiguration 获得一个配置文件的路径，并将其加载到一个配置中。
func LoadStandaloneConfiguration(config string) (*Configuration, string, error) {
	_, err := os.Stat(config)
	if err != nil {
		return nil, "", err
	}

	b, err := os.ReadFile(config)
	if err != nil {
		return nil, "", err
	}

	// 从yaml中解析环境变量,将环境变量中的值 替换yaml文件中的变量
	b = []byte(os.ExpandEnv(string(b)))

	// 加载默认配置
	conf := LoadDefaultConfiguration()
	err = yaml.Unmarshal(b, conf)
	if err != nil {
		return nil, string(b), err
	}
	// 验证secret的配置，如果存在的话，对允许和拒绝的列表进行排序。
	err = sortAndValidateSecretsConfiguration(conf)
	if err != nil {
		return nil, string(b), err
	}

	return conf, string(b), nil
}

// LoadKubernetesConfiguration 从Kubernetes运营商那里获得配置，并给定一个名称
func LoadKubernetesConfiguration(config, namespace string, operatorClient operatorv1pb.OperatorClient) (*Configuration, error) {
	// todo 这个文件到底有什么功能啊,都找不到在哪里创建的
	//apiVersion: dapr.io/v1alpha1
	//kind: Configuration
	//metadata:
	//  annotations:
	//      manager: kubectl-client-side-apply
	//      operation: Update
	//  name: appconfig
	//  namespace: mesoid
	//spec:
	//  metric:
	//    enabled: true
	//  tracing:
	//    samplingRate: '1'
	//    zipkin:
	//      endpointAddress: 'http://zipkin.mesoid.svc.cluster.local:9411/api/v2/spans'

	// 这里与 operator 进行了通信
	// controlPlaneAddress dapr-api.dapr-system.svc.cluster.local:80
	// todo 待看
	resp, err := operatorClient.GetConfiguration(context.Background(),
		&operatorv1pb.GetConfigurationRequest{
			Name:      config,
			Namespace: namespace,
		},
		grpc_retry.WithMax(operatorMaxRetries),              // 重试次数
		grpc_retry.WithPerRetryTimeout(operatorCallTimeout), // 每次连接超时时间
	)
	if err != nil {
		return nil, err
	}
	if resp.GetConfiguration() == nil {
		return nil, errors.Errorf("configuration %s not found", config)
	}
	conf := LoadDefaultConfiguration()                  // 加载默认配置
	err = json.Unmarshal(resp.GetConfiguration(), conf) // 会覆写
	if err != nil {
		return nil, err
	}

	err = sortAndValidateSecretsConfiguration(conf) // 排序和验证 秘钥 配置
	if err != nil {
		return nil, err
	}

	return conf, nil
}

// 验证secret的配置，如果存在的话，对允许和拒绝的列表进行排序。
func sortAndValidateSecretsConfiguration(conf *Configuration) error {
	scopes := conf.Spec.Secrets.Scopes
	set := sets.NewString()
	for _, scope := range scopes {
		// validate scope
		if set.Has(scope.StoreName) {
			return errors.Errorf("%q storeName is repeated in secrets configuration", scope.StoreName)
		}
		if scope.DefaultAccess != "" &&
			!strings.EqualFold(scope.DefaultAccess, AllowAccess) &&
			!strings.EqualFold(scope.DefaultAccess, DenyAccess) {
			return errors.Errorf("defaultAccess %q can be either allow or deny", scope.DefaultAccess)
		}
		set.Insert(scope.StoreName)

		// modify scope
		sort.Strings(scope.AllowedSecrets)
		sort.Strings(scope.DeniedSecrets)
	}

	return nil
}

// IsSecretAllowed 检查是否允许访问这个secret
func (c SecretsScope) IsSecretAllowed(key string) bool {
	var access string = AllowAccess
	if strings.EqualFold(c.DefaultAccess, DenyAccess) {
		access = DenyAccess
	}

	// 如果allowwedsecrets列表不为空，那么检查是否特别允许对该密钥进行访问。
	if len(c.AllowedSecrets) != 0 {
		return containsKey(c.AllowedSecrets, key)
	}

	// 如果秘密存储中存在拒绝列表，请在拒绝列表中检查密钥。如果指定的密钥被拒绝，则单独拒绝访问。
	if deny := containsKey(c.DeniedSecrets, key); deny {
		return !deny
	}

	// 检查是否允许定义的默认访问。
	return access == AllowAccess
}

// 在一个排序的字符串列表上运行二进制搜索，以找到一个键。
func containsKey(s []string, key string) bool {
	index := sort.SearchStrings(s, key)

	return index < len(s) && s[index] == key
}

//IsFeatureEnabled 是否启用了该功能
func IsFeatureEnabled(features []FeatureSpec, target Feature) bool {
	for _, feature := range features {
		if feature.Name == target {
			return feature.Enabled
		}
	}
	return false
}
