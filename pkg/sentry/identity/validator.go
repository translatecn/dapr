package identity

// Validator 用于通过使用ID和令牌来验证证书申请者的身份。
type Validator interface {
	Validate(id, token, namespace string) error
}
