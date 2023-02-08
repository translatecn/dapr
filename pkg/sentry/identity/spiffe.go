package identity

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
)

// CreateSPIFFEID 返回给定信任域、命名空间和appID的SPIFFE标准唯一ID。
func CreateSPIFFEID(trustDomain, namespace, appID string) (string, error) {
	if trustDomain == "" {
		return "", errors.New("can't create spiffe id: trust domain is empty")
	}
	if namespace == "" {
		return "", errors.New("can't create spiffe id: namespace is empty")
	}
	if appID == "" {
		return "", errors.New("can't create spiffe id: app id is empty")
	}

	// 根据SPIFFE规范进行验证
	if strings.Contains(trustDomain, ":") {
		return "", errors.New("trust domain cannot contain the : character")
	}
	if len([]byte(trustDomain)) > 255 {
		return "", errors.New("trust domain cannot exceed 255 bytes")
	}

	id := fmt.Sprintf("spiffe://%s/ns/%s/%s", trustDomain, namespace, appID)
	if len([]byte(id)) > 2048 {
		return "", errors.New("spiffe id cannot exceed 2048 bytes")
	}
	return id, nil
}
