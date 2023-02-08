package pubsub

type Subscription struct {
	PubsubName string            `json:"pubsubname"`
	Topic      string            `json:"topic"`
	Metadata   map[string]string `json:"metadata"`
	Rules      []*Rule           `json:"rules,omitempty"` // 将数据发往哪些url
	Scopes     []string          `json:"scopes"`          // 作用于哪些应用ID
}

type Rule struct {
	Match Expr   `json:"match"`
	Path  string `json:"path"`
}

type Expr interface {
	Eval(variables map[string]interface{}) (interface{}, error)
}
