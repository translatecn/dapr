// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package encryption

import (
	"bytes"
	b64 "encoding/base64"

	"github.com/pkg/errors"
)

var encryptedStateStores = map[string]ComponentEncryptionKeys{}

const (
	separator = "||"
)

// AddEncryptedStateStore 将一个加密的状态存储和一个相关的加密密钥添加到一个列表中。
func AddEncryptedStateStore(storeName string, keys ComponentEncryptionKeys) bool {
	if _, ok := encryptedStateStores[storeName]; ok {
		return false
	}

	encryptedStateStores[storeName] = keys
	return true
}

// EncryptedStateStore 返回一个表示状态存储是否支持加密的bool。
func EncryptedStateStore(storeName string) bool {
	_, ok := encryptedStateStores[storeName]
	return ok
}

// TryEncryptValue 将尝试对一个字节数组进行加密，如果状态存储有相关的加密密钥。该函数将把密钥的名称附加到值上，以便以后提取。如果不存在加密密钥，该函数将返回未修改的字节。
func TryEncryptValue(storeName string, value []byte) ([]byte, error) {
	keys := encryptedStateStores[storeName]
	enc, err := encrypt(value, keys.Primary, AES256Algorithm)
	if err != nil {
		return value, err
	}

	sEnc := b64.StdEncoding.EncodeToString(enc) + separator + keys.Primary.Name
	return []byte(sEnc), nil
}

// TryDecryptValue 如果状态存储有相关的加密密钥，将尝试解密一个字节数组。如果不存在加密密钥，该函数将返回未修改的字节。
func TryDecryptValue(storeName string, value []byte) ([]byte, error) {
	keys := encryptedStateStores[storeName]
	// 提取应附加在值上的解密密钥
	ind := bytes.LastIndex(value, []byte(separator))
	keyName := string(value[ind+len(separator):])

	if len(keyName) == 0 {
		return value, errors.Errorf("无法为状态存储解密数据 %s: 记录中没有找到加密密钥名称", storeName)
	}

	var key Key

	if keys.Primary.Name == keyName {
		key = keys.Primary
	} else if keys.Secondary.Name == keyName {
		key = keys.Secondary
	}

	return decrypt(value[:ind], key, AES256Algorithm)
}
