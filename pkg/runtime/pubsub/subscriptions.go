package pubsub

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	subscriptionsapi_v1alpha1 "github.com/dapr/dapr/pkg/apis/subscriptions/v1alpha1"
	subscriptionsapi_v2alpha1 "github.com/dapr/dapr/pkg/apis/subscriptions/v2alpha1"
	"github.com/dapr/dapr/pkg/channel"
	"github.com/dapr/dapr/pkg/expr"
	invokev1 "github.com/dapr/dapr/pkg/messaging/v1"
	operatorv1pb "github.com/dapr/dapr/pkg/proto/operator/v1"
	runtimev1pb "github.com/dapr/dapr/pkg/proto/runtime/v1"
	"github.com/dapr/kit/logger"
)

const (
	getTopicsError         = "从应用程序获取主题列表的错误: %s"
	deserializeTopicsError = "从应用程序获取主题的错误: %s"
	noSubscriptionsError   = "用户应用程序没有订阅任何主题"
	subscriptionKind       = "Subscription"

	APIVersionV1alpha1 = "dapr.io/v1alpha1"
	APIVersionV2alpha1 = "dapr.io/v2alpha1"
)

type (
	SubscriptionJSON struct {
		PubsubName string            `json:"pubsubname"`         // 组件名称
		Topic      string            `json:"topic"`              // 主题名称
		Metadata   map[string]string `json:"metadata,omitempty"` // 元信息
		Route      string            `json:"route"`              // Single route from v1alpha1
		Routes     RoutesJSON        `json:"routes"`             // Multiple routes from v2alpha1
	}

	RoutesJSON struct {
		Rules   []*RuleJSON `json:"rules,omitempty"`
		Default string      `json:"default,omitempty"`
	}

	RuleJSON struct {
		Match string `json:"match"`
		Path  string `json:"path"`
	}
)

//GetSubscriptionsHTTP 通过http获取订阅
func GetSubscriptionsHTTP(channel channel.AppChannel, log logger.Logger) ([]Subscription, error) {
	var subscriptions []Subscription
	var subscriptionItems []SubscriptionJSON
	log.Info("获取应用程序关于subscribe的配置")
	req := invokev1.NewInvokeMethodRequest("dapr/subscribe")
	req.WithHTTPExtension(http.MethodGet, "")
	req.WithRawData(nil, invokev1.JSONContentType)

	//   传播 Context
	ctx := context.Background()
	resp, err := channel.InvokeMethod(ctx, req)
	if err != nil {
		log.Errorf(getTopicsError, err)

		return nil, err
	}

	switch resp.Status().Code {
	case http.StatusOK:
		_, body := resp.RawData()
		if err := json.Unmarshal(body, &subscriptionItems); err != nil {
			log.Errorf(deserializeTopicsError, err)

			return nil, errors.Errorf(deserializeTopicsError, err)
		}
		subscriptions = make([]Subscription, len(subscriptionItems))
		for i, si := range subscriptionItems {
			// 寻找单一的路由字段并将其作为路由结构附加。这保留了向后的兼容性。

			rules := make([]*Rule, 0, len(si.Routes.Rules)+1)
			for _, r := range si.Routes.Rules {
				rule, err := createRoutingRule(r.Match, r.Path)
				if err != nil {
					return nil, err
				}
				rules = append(rules, rule)
			}

			// 如果设置了默认路径，则添加一个带有nil `Match`的规则，该规则被视为`true`，如果之前的规则都不匹配，则总是被选中。
			if si.Routes.Default != "" {
				rules = append(rules, &Rule{
					Path: si.Routes.Default,
				})
			} else if si.Route != "" {
				rules = append(rules, &Rule{
					Path: si.Route,
				})
			}

			subscriptions[i] = Subscription{
				PubsubName: si.PubsubName,
				Topic:      si.Topic,
				Metadata:   si.Metadata,
				Rules:      rules,
			}
		}

	case http.StatusNotFound:
		log.Debug(noSubscriptionsError)

	default:
		// 意外的响应：GRPC和HTTP都要记录相同的级别。
		log.Errorf("应用程序从订阅端点返回http状态代码%v", resp.Status().Code)
	}

	log.Debugf("应用程序回应订阅 %v", subscriptions)

	return filterSubscriptions(subscriptions, log), nil
}

// 过滤没有设置路由的
func filterSubscriptions(subscriptions []Subscription, log logger.Logger) []Subscription {
	for i := len(subscriptions) - 1; i >= 0; i-- {
		if len(subscriptions[i].Rules) == 0 {
			log.Warnf("topic %s 有一个空的路由。 从订阅列表中删除", subscriptions[i].Topic)
			subscriptions = append(subscriptions[:i], subscriptions[i+1:]...)
		}
	}

	return subscriptions
}

func GetSubscriptionsGRPC(channel runtimev1pb.AppCallbackClient, log logger.Logger) ([]Subscription, error) {
	var subscriptions []Subscription

	resp, err := channel.ListTopicSubscriptions(context.Background(), &emptypb.Empty{})
	if err != nil {
		// Unexpected response: both GRPC and HTTP have to log the same level.
		log.Errorf(getTopicsError, err)
	} else {
		if resp == nil || resp.Subscriptions == nil || len(resp.Subscriptions) == 0 {
			log.Debug(noSubscriptionsError)
		} else {
			for _, s := range resp.Subscriptions {
				rules, err := parseRoutingRulesGRPC(s.Routes)
				if err != nil {
					return nil, err
				}
				subscriptions = append(subscriptions, Subscription{
					PubsubName: s.PubsubName,
					Topic:      s.GetTopic(),
					Metadata:   s.GetMetadata(),
					Rules:      rules,
				})
			}
		}
	}

	return subscriptions, nil
}

// DeclarativeSelfHosted 从给定的组件路径加载订阅。
func DeclarativeSelfHosted(componentsPath string, log logger.Logger) []Subscription {
	var subs []Subscription

	if _, err := os.Stat(componentsPath); os.IsNotExist(err) {
		return subs
	}

	files, err := os.ReadDir(componentsPath)
	if err != nil {
		log.Errorf("未能从路径中读取订阅信息 %s: %s", err)
		return subs
	}

	for _, f := range files {
		if !f.IsDir() {
			filePath := filepath.Join(componentsPath, f.Name())
			b, err := os.ReadFile(filePath)
			if err != nil {
				log.Errorf("读取文件失败 %s: %s", filePath, err)
				continue
			}

			subs, err = appendSubscription(subs, b)
			if err != nil {
				log.Warnf("未能从文件中添加订阅 %s: %s", filePath, err)
				continue
			}
		}
	}

	return subs
}

// 解析组件数据，转换成Subscription结构体
func marshalSubscription(b []byte) (*Subscription, error) {
	// 1、首先只解析类型元数据
	type typeInfo struct {
		metav1.TypeMeta `json:",inline"`
	}

	var ti typeInfo
	if err := yaml.Unmarshal(b, &ti); err != nil {
		return nil, err
	}
	// 判断解析后的组件是不订阅组件
	if ti.Kind != subscriptionKind {
		return nil, nil
	}

	switch ti.APIVersion {
	case APIVersionV2alpha1:
		// "v2alpha1 "是引入pubsub路由的CRD。
		var sub subscriptionsapi_v2alpha1.Subscription
		if err := yaml.Unmarshal(b, &sub); err != nil {
			return nil, err
		}

		rules, err := parseRoutingRulesYAML(sub.Spec.Routes)
		if err != nil {
			return nil, err
		}

		return &Subscription{
			Topic:      sub.Spec.Topic,
			PubsubName: sub.Spec.Pubsubname,
			Rules:      rules,
			Metadata:   sub.Spec.Metadata,
			Scopes:     sub.Scopes,
		}, nil

	default:
		// 假设 "v1alpha1 "是为了向后兼容，因为在引入 "v2alpha "之前没有检查过这个。
		var sub subscriptionsapi_v1alpha1.Subscription
		if err := yaml.Unmarshal(b, &sub); err != nil {
			return nil, err
		}

		return &Subscription{
			Topic:      sub.Spec.Topic,
			PubsubName: sub.Spec.Pubsubname,
			Rules: []*Rule{
				{
					Path: sub.Spec.Route,
				},
			},
			Metadata: sub.Spec.Metadata,
			Scopes:   sub.Scopes,
		}, nil
	}
}

// 从yaml文件中解析出路由规则
func parseRoutingRulesYAML(routes subscriptionsapi_v2alpha1.Routes) ([]*Rule, error) {
	r := make([]*Rule, 0, len(routes.Rules)+1)

	for _, rule := range routes.Rules {
		rr, err := createRoutingRule(rule.Match, rule.Path)
		if err != nil {
			return nil, err
		}
		r = append(r, rr)
	}

	// If a default path is set, add a rule with a nil `Match`,
	// which is treated as `true` and always selected if
	// no previous rules match.
	if routes.Default != "" {
		r = append(r, &Rule{
			Path: routes.Default,
		})
	}

	return r, nil
}

func parseRoutingRulesGRPC(routes *runtimev1pb.TopicRoutes) ([]*Rule, error) {
	if routes == nil {
		return []*Rule{{
			Path: "",
		}}, nil
	}
	r := make([]*Rule, 0, len(routes.Rules)+1)

	for _, rule := range routes.Rules {
		rr, err := createRoutingRule(rule.Match, rule.Path)
		if err != nil {
			return nil, err
		}
		r = append(r, rr)
	}

	// If a default path is set, add a rule with a nil `Match`,
	// which is treated as `true` and always selected if
	// no previous rules match.
	if routes.Default != "" {
		r = append(r, &Rule{
			Path: routes.Default,
		})
	}

	// gRPC automatically specifies a default route
	// if none are returned.
	if len(r) == 0 {
		r = append(r, &Rule{
			Path: "",
		})
	}

	return r, nil
}

// 创建路由匹配规则
func createRoutingRule(match, path string) (*Rule, error) {
	var e *expr.Expr
	matchTrimmed := strings.TrimSpace(match)
	if matchTrimmed != "" {
		e = &expr.Expr{}
		if err := e.DecodeString(matchTrimmed); err != nil {
			return nil, err
		}
	}

	return &Rule{
		Match: e,
		Path:  path,
	}, nil
}

// DeclarativeKubernetes 在Kubernetes中运行时，从operator加载订阅。
func DeclarativeKubernetes(client operatorv1pb.OperatorClient, log logger.Logger) []Subscription {
	var subs []Subscription
	resp, err := client.ListSubscriptions(context.TODO(), &emptypb.Empty{})
	if err != nil {
		log.Errorf("未能从operator那里列出订阅信息: %s", err)

		return subs
	}

	for _, s := range resp.Subscriptions {
		subs, err = appendSubscription(subs, s)
		if err != nil {
			log.Warnf("未能从操作员处添加订阅: %s", err)
			continue
		}
	}

	return subs
}

//OK
func appendSubscription(list []Subscription, subBytes []byte) ([]Subscription, error) {
	sub, err := marshalSubscription(subBytes)
	if err != nil {
		return nil, err
	}

	if sub != nil {
		list = append(list, *sub)
	}

	return list, nil
}
