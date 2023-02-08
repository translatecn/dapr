package diagnostics

import (
	"context"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	diag_utils "github.com/dapr/dapr/pkg/diagnostics/utils"
)

// Tag keys.
var (
	componentKey    = tag.MustNewKey("component")
	failReasonKey   = tag.MustNewKey("reason")
	operationKey    = tag.MustNewKey("operation")
	actorTypeKey    = tag.MustNewKey("actor_type")
	trustDomainKey  = tag.MustNewKey("trustDomain")
	namespaceKey    = tag.MustNewKey("namespace")
	policyActionKey = tag.MustNewKey("policyAction")
)

// serviceMetrics 持有dapr运行时度量监测方法。
type serviceMetrics struct {
	// 组件 指标
	componentLoaded        *stats.Int64Measure
	componentInitCompleted *stats.Int64Measure
	componentInitFailed    *stats.Int64Measure

	// mTLS 指标
	mtlsInitCompleted             *stats.Int64Measure
	mtlsInitFailed                *stats.Int64Measure
	mtlsWorkloadCertRotated       *stats.Int64Measure
	mtlsWorkloadCertRotatedFailed *stats.Int64Measure

	// Actor 指标
	actorStatusReportTotal       *stats.Int64Measure
	actorStatusReportFailedTotal *stats.Int64Measure
	actorTableOperationRecvTotal *stats.Int64Measure
	actorRebalancedTotal         *stats.Int64Measure
	actorDeactivationTotal       *stats.Int64Measure
	actorDeactivationFailedTotal *stats.Int64Measure
	actorPendingCalls            *stats.Int64Measure

	//访问控制列表和服务调用指标
	appPolicyActionAllowed    *stats.Int64Measure
	globalPolicyActionAllowed *stats.Int64Measure
	appPolicyActionBlocked    *stats.Int64Measure
	globalPolicyActionBlocked *stats.Int64Measure

	appID   string
	ctx     context.Context
	enabled bool
}

// newServiceMetrics 返回带有默认服务指标统计信息的serviceMetrics实例。
func newServiceMetrics() *serviceMetrics {
	return &serviceMetrics{
		// 运行时组件指标
		componentLoaded:        stats.Int64("runtime/component/loaded", "The number of successfully loaded components.", stats.UnitDimensionless),
		componentInitCompleted: stats.Int64("runtime/component/init_total", "The number of initialized components.", stats.UnitDimensionless),
		componentInitFailed:    stats.Int64("runtime/component/init_fail_total", "The number of component initialization failures.", stats.UnitDimensionless),

		// mTLS
		mtlsInitCompleted:             stats.Int64("runtime/mtls/init_total", "The number of successful mTLS authenticator initialization.", stats.UnitDimensionless),
		mtlsInitFailed:                stats.Int64("runtime/mtls/init_fail_total", "The number of mTLS authenticator init failures.", stats.UnitDimensionless),
		mtlsWorkloadCertRotated:       stats.Int64("runtime/mtls/workload_cert_rotated_total", "The number of the successful workload certificate rotations.", stats.UnitDimensionless),
		mtlsWorkloadCertRotatedFailed: stats.Int64("runtime/mtls/workload_cert_rotated_fail_total", "The number of the failed workload certificate rotations.", stats.UnitDimensionless),

		// Actor
		actorStatusReportTotal:       stats.Int64("runtime/actor/status_report_total", "The number of the successful status reports to placement service.", stats.UnitDimensionless),
		actorStatusReportFailedTotal: stats.Int64("runtime/actor/status_report_fail_total", "The number of the failed status reports to placement service.", stats.UnitDimensionless),
		actorTableOperationRecvTotal: stats.Int64("runtime/actor/table_operation_recv_total", "The number of the received actor placement table operations.", stats.UnitDimensionless),
		actorRebalancedTotal:         stats.Int64("runtime/actor/rebalanced_total", "The number of the actor rebalance requests.", stats.UnitDimensionless),
		actorDeactivationTotal:       stats.Int64("runtime/actor/deactivated_total", "The number of the successful actor deactivation.", stats.UnitDimensionless),
		actorDeactivationFailedTotal: stats.Int64("runtime/actor/deactivated_failed_total", "The number of the failed actor deactivation.", stats.UnitDimensionless),
		actorPendingCalls:            stats.Int64("runtime/actor/pending_actor_calls", "The number of pending actor calls waiting to acquire the per-actor lock.", stats.UnitDimensionless),

		// Access Control Lists for service invocation
		appPolicyActionAllowed:    stats.Int64("runtime/acl/app_policy_action_allowed_total", "The number of requests allowed by the app specific action specified in the access control policy.", stats.UnitDimensionless),
		globalPolicyActionAllowed: stats.Int64("runtime/acl/global_policy_action_allowed_total", "The number of requests allowed by the global action specified in the access control policy.", stats.UnitDimensionless),
		appPolicyActionBlocked:    stats.Int64("runtime/acl/app_policy_action_blocked_total", "The number of requests blocked by the app specific action specified in the access control policy.", stats.UnitDimensionless),
		globalPolicyActionBlocked: stats.Int64("runtime/acl/global_policy_action_blocked_total", "The number of requests blocked by the global action specified in the access control policy.", stats.UnitDimensionless),

		// TODO: use the correct context for each request
		ctx:     context.Background(),
		enabled: false,
	}
}

// Init
// 初始化度量指标
func (s *serviceMetrics) Init(appID string) error {
	s.appID = appID
	s.enabled = true
	return view.Register(
		diag_utils.NewMeasureView(s.componentLoaded, []tag.Key{appIDKey}, view.Count()),
		diag_utils.NewMeasureView(s.componentInitCompleted, []tag.Key{appIDKey, componentKey}, view.Count()),
		diag_utils.NewMeasureView(s.componentInitFailed, []tag.Key{appIDKey, componentKey, failReasonKey}, view.Count()),

		diag_utils.NewMeasureView(s.mtlsInitCompleted, []tag.Key{appIDKey}, view.Count()),
		diag_utils.NewMeasureView(s.mtlsInitFailed, []tag.Key{appIDKey, failReasonKey}, view.Count()),
		diag_utils.NewMeasureView(s.mtlsWorkloadCertRotated, []tag.Key{appIDKey}, view.Count()),
		diag_utils.NewMeasureView(s.mtlsWorkloadCertRotatedFailed, []tag.Key{appIDKey, failReasonKey}, view.Count()),

		diag_utils.NewMeasureView(s.actorStatusReportTotal, []tag.Key{appIDKey, actorTypeKey, operationKey}, view.Count()),
		diag_utils.NewMeasureView(s.actorStatusReportFailedTotal, []tag.Key{appIDKey, actorTypeKey, operationKey, failReasonKey}, view.Count()),
		diag_utils.NewMeasureView(s.actorTableOperationRecvTotal, []tag.Key{appIDKey, actorTypeKey, operationKey}, view.Count()),
		diag_utils.NewMeasureView(s.actorRebalancedTotal, []tag.Key{appIDKey, actorTypeKey}, view.Count()),
		diag_utils.NewMeasureView(s.actorDeactivationTotal, []tag.Key{appIDKey, actorTypeKey}, view.Count()),
		diag_utils.NewMeasureView(s.actorDeactivationFailedTotal, []tag.Key{appIDKey, actorTypeKey}, view.Count()),
		diag_utils.NewMeasureView(s.actorPendingCalls, []tag.Key{appIDKey, actorTypeKey}, view.LastValue()),

		diag_utils.NewMeasureView(s.appPolicyActionAllowed, []tag.Key{appIDKey, trustDomainKey, namespaceKey, operationKey, httpMethodKey, policyActionKey}, view.LastValue()),
		diag_utils.NewMeasureView(s.globalPolicyActionAllowed, []tag.Key{appIDKey, trustDomainKey, namespaceKey, operationKey, httpMethodKey, policyActionKey}, view.LastValue()),
		diag_utils.NewMeasureView(s.appPolicyActionBlocked, []tag.Key{appIDKey, trustDomainKey, namespaceKey, operationKey, httpMethodKey, policyActionKey}, view.LastValue()),
		diag_utils.NewMeasureView(s.globalPolicyActionBlocked, []tag.Key{appIDKey, trustDomainKey, namespaceKey, operationKey, httpMethodKey, policyActionKey}, view.LastValue()),
	)
}

// ComponentLoaded 当组件被成功加载时记录指标。
func (s *serviceMetrics) ComponentLoaded() {
	if s.enabled {
		stats.RecordWithTags(s.ctx, diag_utils.WithTags(appIDKey, s.appID), s.componentLoaded.M(1))
	}
}

// ComponentInitialized 当组件被成功初始化记录指标。
func (s *serviceMetrics) ComponentInitialized(component string) {
	if s.enabled {
		stats.RecordWithTags(
			s.ctx,
			diag_utils.WithTags(appIDKey, s.appID, componentKey, component),
			s.componentInitCompleted.M(1))
	}
}

// ComponentInitFailed 当组件失败加载时记录指标
func (s *serviceMetrics) ComponentInitFailed(component string, reason string) {
	if s.enabled {
		stats.RecordWithTags(
			s.ctx,
			diag_utils.WithTags(appIDKey, s.appID, componentKey, component, failReasonKey, reason),
			s.componentInitFailed.M(1))
	}
}

// MTLSInitCompleted 当组件被成功初始化记录指标
func (s *serviceMetrics) MTLSInitCompleted() {
	if s.enabled {
		stats.RecordWithTags(s.ctx, diag_utils.WithTags(appIDKey, s.appID), s.mtlsInitCompleted.M(1))
	}
}

// MTLSInitFailed 当组件失败加载时记录指标
func (s *serviceMetrics) MTLSInitFailed(reason string) {
	if s.enabled {
		stats.RecordWithTags(
			s.ctx, diag_utils.WithTags(appIDKey, s.appID, failReasonKey, reason),
			s.mtlsInitFailed.M(1))
	}
}

// MTLSWorkLoadCertRotationCompleted 当工作负载证书轮换成功后，记录指标。
func (s *serviceMetrics) MTLSWorkLoadCertRotationCompleted() {
	if s.enabled {
		stats.RecordWithTags(s.ctx, diag_utils.WithTags(appIDKey, s.appID), s.mtlsWorkloadCertRotated.M(1))
	}
}

// MTLSWorkLoadCertRotationFailed 当工作负载证书轮换失败后，记录指标。
func (s *serviceMetrics) MTLSWorkLoadCertRotationFailed(reason string) {
	if s.enabled {
		stats.RecordWithTags(
			s.ctx, diag_utils.WithTags(appIDKey, s.appID, failReasonKey, reason),
			s.mtlsWorkloadCertRotatedFailed.M(1))
	}
}

// ActorStatusReported 向placement报告状态时，记录指标。
func (s *serviceMetrics) ActorStatusReported(operation string) {
	if s.enabled {
		stats.RecordWithTags(
			s.ctx, diag_utils.WithTags(appIDKey, s.appID, operationKey, operation),
			s.actorStatusReportTotal.M(1))
	}
}

// ActorStatusReportFailed 当向placement状态报告失败时，记录指标。
func (s *serviceMetrics) ActorStatusReportFailed(operation string, reason string) {
	if s.enabled {
		stats.RecordWithTags(
			s.ctx, diag_utils.WithTags(appIDKey, s.appID, operationKey, operation, failReasonKey, reason),
			s.actorStatusReportFailedTotal.M(1))
	}
}

// ActorPlacementTableOperationReceived 当运行时收到表操作时，记录度量。
func (s *serviceMetrics) ActorPlacementTableOperationReceived(operation string) {
	if s.enabled {
		stats.RecordWithTags(
			s.ctx, diag_utils.WithTags(appIDKey, s.appID, operationKey, operation),
			s.actorTableOperationRecvTotal.M(1))
	}
}

// ActorRebalanced 当actors被榨干时，记录度量。
func (s *serviceMetrics) ActorRebalanced(actorType string) {
	if s.enabled {
		stats.RecordWithTags(
			s.ctx,
			diag_utils.WithTags(appIDKey, s.appID, actorTypeKey, actorType),
			s.actorRebalancedTotal.M(1))
	}
}

// ActorDeactivated 当actor被停用时，记录指标。
func (s *serviceMetrics) ActorDeactivated(actorType string) {
	if s.enabled {
		stats.RecordWithTags(
			s.ctx,
			diag_utils.WithTags(appIDKey, s.appID, actorTypeKey, actorType),
			s.actorDeactivationTotal.M(1))
	}
}

// ActorDeactivationFailed 当actor停用失败时，记录指标。
func (s *serviceMetrics) ActorDeactivationFailed(actorType, reason string) {
	if s.enabled {
		stats.RecordWithTags(
			s.ctx,
			diag_utils.WithTags(appIDKey, s.appID, actorTypeKey, actorType, failReasonKey, reason),
			s.actorDeactivationFailedTotal.M(1))
	}
}

// ReportActorPendingCalls 记录当前阻塞中的actor
func (s *serviceMetrics) ReportActorPendingCalls(actorType string, pendingLocks int32) {
	if s.enabled {
		stats.RecordWithTags(
			s.ctx,
			diag_utils.WithTags(appIDKey, s.appID, actorTypeKey, actorType),
			s.actorPendingCalls.M(int64(pendingLocks)))
	}
}

// RequestAllowedByAppAction 记录由于与应用程序的访问控制策略中指定的行动相匹配而允许的请求。
func (s *serviceMetrics) RequestAllowedByAppAction(appID, trustDomain, namespace, operation, httpverb string, policyAction bool) {
	if s.enabled {
		stats.RecordWithTags(
			s.ctx,
			diag_utils.WithTags(
				appIDKey, appID,
				trustDomainKey, trustDomain,
				namespaceKey, namespace,
				operationKey, operation,
				httpMethodKey, httpverb,
				policyActionKey, policyAction),
			s.appPolicyActionAllowed.M(1))
	}
}

// RequestBlockedByAppAction 记录由于与应用程序的访问控制策略中指定的行动相匹配而拒绝的请求
func (s *serviceMetrics) RequestBlockedByAppAction(appID, trustDomain, namespace, operation, httpverb string, policyAction bool) {
	if s.enabled {
		stats.RecordWithTags(
			s.ctx,
			diag_utils.WithTags(
				appIDKey, appID,
				trustDomainKey, trustDomain,
				namespaceKey, namespace,
				operationKey, operation,
				httpMethodKey, httpverb,
				policyActionKey, policyAction),
			s.appPolicyActionAllowed.M(1))
	}
}

// RequestAllowedByGlobalAction 记录由于与全局的行动相匹配而允许的请求
func (s *serviceMetrics) RequestAllowedByGlobalAction(appID, trustDomain, namespace, operation, httpverb string, policyAction bool) {
	if s.enabled {
		stats.RecordWithTags(
			s.ctx,
			diag_utils.WithTags(
				appIDKey, appID,
				trustDomainKey, trustDomain,
				namespaceKey, namespace,
				operationKey, operation,
				httpMethodKey, httpverb,
				policyActionKey, policyAction),
			s.globalPolicyActionAllowed.M(1))
	}
}

// RequestBlockedByGlobalAction 记录由于与全局的行动相匹配而允许的请求
func (s *serviceMetrics) RequestBlockedByGlobalAction(appID, trustDomain, namespace, operation, httpverb string, policyAction bool) {
	if s.enabled {
		stats.RecordWithTags(
			s.ctx,
			diag_utils.WithTags(
				appIDKey, appID,
				trustDomainKey, trustDomain,
				namespaceKey, namespace,
				operationKey, operation,
				httpMethodKey, httpverb,
				policyActionKey, policyAction),
			s.globalPolicyActionBlocked.M(1))
	}
}
