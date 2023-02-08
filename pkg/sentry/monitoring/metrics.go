package monitoring

import (
	"context"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	diag_utils "github.com/dapr/dapr/pkg/diagnostics/utils"
)

var (
	// Metrics definitions.
	csrReceivedTotal              = stats.Int64("sentry/cert/sign/request_received_total", "收到的CSR的数量。", stats.UnitDimensionless)
	certSignSuccessTotal          = stats.Int64("sentry/cert/sign/success_total", "已成功发行的证书数量。", stats.UnitDimensionless)
	certSignFailedTotal           = stats.Int64("sentry/cert/sign/failure_total", "签署CSR时发生的错误数量。", stats.UnitDimensionless)
	serverTLSCertIssueFailedTotal = stats.Int64("sentry/servercert/issue_failed_total", "服务器TLS证书发放失败的次数。", stats.UnitDimensionless)
	issuerCertChangedTotal        = stats.Int64("sentry/issuercert/changed_total", "当签发人的证书或钥匙被改变时，签发人证书更新的数量", stats.UnitDimensionless)
	issuerCertExpiryTimestamp     = stats.Int64("sentry/issuercert/expiry_timestamp", "发行人/根证书过期的unix时间戳，单位是秒。", stats.UnitDimensionless)

	// Metrics Tags.
	failedReasonKey = tag.MustNewKey("reason")
	noKeys          = []tag.Key{}
)

// CertSignRequestReceived 收到CSR的计数。
func CertSignRequestReceived() {
	stats.Record(context.Background(), csrReceivedTotal.M(1))
}

// CertSignSucceed  成功签发了证书的数目
func CertSignSucceed() {
	stats.Record(context.Background(), certSignSuccessTotal.M(1))
}

// CertSignFailed 失败签发了证书的数目
func CertSignFailed(reason string) {
	stats.RecordWithTags(
		context.Background(),
		diag_utils.WithTags(failedReasonKey, reason),
		certSignFailedTotal.M(1))
}

// IssuerCertExpiry 记录根证书的到期情况。
func IssuerCertExpiry(expiry time.Time) {
	stats.Record(context.Background(), issuerCertExpiryTimestamp.M(expiry.Unix()))
}

// ServerCertIssueFailed 记录服务器证书问题失败。
func ServerCertIssueFailed(reason string) {
	stats.Record(context.Background(), serverTLSCertIssueFailedTotal.M(1))
}

// IssuerCertChanged 记录发行人凭证变更。
func IssuerCertChanged() {
	stats.Record(context.Background(), issuerCertChangedTotal.M(1))
}

// InitMetrics 初始化指标
func InitMetrics() error {
	return view.Register(
		diag_utils.NewMeasureView(csrReceivedTotal, noKeys, view.Count()),
		diag_utils.NewMeasureView(certSignSuccessTotal, noKeys, view.Count()),
		diag_utils.NewMeasureView(certSignFailedTotal, []tag.Key{failedReasonKey}, view.Count()),
		diag_utils.NewMeasureView(serverTLSCertIssueFailedTotal, []tag.Key{failedReasonKey}, view.Count()),
		diag_utils.NewMeasureView(issuerCertChangedTotal, noKeys, view.Count()),
		diag_utils.NewMeasureView(issuerCertExpiryTimestamp, noKeys, view.LastValue()),
	)
}
