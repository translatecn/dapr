package certs

const (
	// KubeScrtName 是持有信任包的kubernetes秘密的名称。
	KubeScrtName = "dapr-trust-bundle"
	// TrustAnchorsEnvVar
	//是sidecar中信任锚的环境变量名。
	TrustAnchorsEnvVar = "DAPR_TRUST_ANCHORS"
	CertChainEnvVar    = "DAPR_CERT_CHAIN"
	CertKeyEnvVar      = "DAPR_CERT_KEY"
)
