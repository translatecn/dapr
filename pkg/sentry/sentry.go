package sentry

import (
	"context"
	"github.com/dapr/dapr/code_debug/replace"
	sentry_debug "github.com/dapr/dapr/code_debug/sentry"
	"github.com/dapr/dapr/pkg/sentry/ca"
	"github.com/dapr/dapr/pkg/sentry/config"
	"github.com/dapr/dapr/pkg/sentry/identity"
	"github.com/dapr/dapr/pkg/sentry/identity/kubernetes"
	"github.com/dapr/dapr/pkg/sentry/identity/selfhosted"
	k8s "github.com/dapr/dapr/pkg/sentry/kubernetes"
	"github.com/dapr/dapr/pkg/sentry/monitoring"
	"github.com/dapr/dapr/pkg/sentry/server"
	"github.com/dapr/kit/logger" // ok
	raw_k8s "k8s.io/client-go/kubernetes"
)

var log = logger.NewLogger("dapr.sentry")

type CertificateAuthority interface {
	Run(context.Context, config.SentryConfig, chan bool)
	Restart(ctx context.Context, conf config.SentryConfig)
}

type sentry struct {
	server    server.CAServer
	reloading bool
}

// NewSentryCA  返回一个Sentry认证的实例
func NewSentryCA() CertificateAuthority {
	return &sentry{}
}

// Run 加载信任锚和颁发者证书，创建新的CA并运行CA服务器。
func (s *sentry) Run(ctx context.Context, conf config.SentryConfig, readyCh chan bool) {
	// Create CA
	certAuth, err := ca.NewCertificateAuthority(conf)
	if err != nil {
		log.Fatalf("error getting certificate authority: %s", err)
	}
	log.Info("certificate authority loaded")

	// 加载信任包
	err = certAuth.LoadOrStoreTrustBundle()
	if err != nil {
		log.Fatalf("error loading trust root bundle: %s", err)
	}
	log.Infof("trust root bundle loaded. issuer cert expiry: %s", certAuth.GetCACertBundle().GetIssuerCertExpiry().String())
	//记录根证书过期时间
	monitoring.IssuerCertExpiry(certAuth.GetCACertBundle().GetIssuerCertExpiry())

	// 创建身份验证器
	v, err := createValidator()
	if err != nil {
		log.Fatalf("error creating validator: %s", err)
	}
	log.Info("validator created")

	// 返回一个运行gRPC服务器的新CA服务器。
	s.server = server.NewCAServer(certAuth, v)

	// 启停启停，这个goroutinue 会不会变得很多
	go func() {
		<-ctx.Done()
		log.Info("sentry certificate authority is shutting down")
		s.server.Shutdown() // nolint: errcheck
	}()
	// 启停启停 只有一个 这个goroutinue 会跑到当前代码，由于  reloading
	if readyCh != nil {
		readyCh <- true
		s.reloading = false
	}

	log.Infof("sentry 认证机构正在运行，保护你们的安全")
	err = s.server.Run(conf.Port, certAuth.GetCACertBundle())
	if err != nil {
		log.Fatalf("error starting gRPC server: %s", err)
	}
}

func createValidator() (identity.Validator, error) {
	if config.IsKubernetesHosted() {
		//  我们在Kubernetes中，创建客户端并启动一个新的服务账户令牌验证器
		// 此处改用加载本地配置文件 ~/.kube/config
		var kubeClient *raw_k8s.Clientset
		if replace.Replace() > 0 {
			kubeClient = sentry_debug.GetK8s()

		} else {
			kubeClient, _ = k8s.GetClient()
		}

		return kubernetes.NewValidator(kubeClient), nil
	}
	return selfhosted.NewValidator(), nil
}

func (s *sentry) Restart(ctx context.Context, conf config.SentryConfig) {
	if s.reloading {
		return
	}
	s.reloading = true

	s.server.Shutdown()
	go s.Run(ctx, conf, nil)
}
