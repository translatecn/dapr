package identity

// Bundle 包含所有的元素，以识别一个跨信任域和命名空间的工作负载。
type Bundle struct {
	ID          string
	Namespace   string
	TrustDomain string
}

// NewBundle 返回一个新的身份包。namespace和trustDomain是可选参数。当为空时，将返回一个零值。
func NewBundle(id, namespace, trustDomain string) *Bundle {
	// 空的命名空间和信任域会导致一个空的Bundle
	if namespace == "" || trustDomain == "" {
		return nil
	}

	return &Bundle{
		ID:          id,
		Namespace:   namespace,
		TrustDomain: trustDomain,
	}
}
