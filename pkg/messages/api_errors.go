package messages

const (
	// Http.
	ErrNotFound             = "method %q is 没有发现"
	ErrMalformedRequest     = "反序列化 HTTP body失败: %s"
	ErrMalformedRequestData = "can't 序列化 request data field: %s"

	// State.
	ErrStateStoresNotConfigured = "状态存储 is 没有配置"
	ErrStateStoreNotFound       = "状态存储 %s 没有发现"
	ErrStateGet                 = "fail to get %s from state store %s: %s"
	ErrStateDelete              = "失败的删除状态与键 %s: %s"
	ErrStateSave                = "在状态存储器中保存状态失败 %s: %s"
	ErrStateQuery               = "在状态存储中查询失败 %s: %s"

	// StateTransaction.
	ErrStateStoreNotSupported     = "状态存储 【%s】 不支持事务"
	ErrNotSupportedStateOperation = "操作 【%s】 不支持"
	ErrStateTransaction           = "执行状态存储事务时产生了错误: %s"

	// Binding.
	ErrInvokeOutputBinding = "error when invoke output binding %s: %s"

	// PubSub.
	ErrPubsubNotConfigured      = "没有pubsub组件配置"
	ErrPubsubEmpty              = "pubsub的怒不敢住不能为空" //     //
	ErrPubsubNotFound           = "pubsub %s 没有发现"
	ErrTopicEmpty               = "pubsub 配置的主题中不存在 %s"
	ErrPubsubCloudEventsSer     = "当序列化事件信息时发生了错误 topic %s pubsub %s: %s"
	ErrPubsubPublishMessage     = "error when publish to topic %s in pubsub %s: %s"
	ErrPubsubForbidden          = "topic %s is not allowed for app id %s"
	ErrPubsubCloudEventCreation = "cannot create cloudevent: %s"

	// AppChannel.
	ErrChannelNotFound       = "app channel is not initialized"
	ErrInternalInvokeRequest = "parsing InternalInvokeRequest error: %s"
	ErrChannelInvoke         = "error invoking app channel: %s"

	// Actor.
	ErrActorRuntimeNotFound      = "actor runtime 没有配置"
	ErrActorInstanceMissing      = "actor instance is missing"
	ErrActorInvoke               = "error invoke actor method: %s"
	ErrActorReminderCreate       = "error creating actor reminder: %s"
	ErrActorReminderGet          = "error getting actor reminder: %s"
	ErrActorReminderDelete       = "error deleting actor reminder: %s"
	ErrActorTimerCreate          = "error creating actor timer: %s"
	ErrActorTimerDelete          = "error deleting actor timer: %s"
	ErrActorStateGet             = "error getting actor state: %s"
	ErrActorStateTransactionSave = "error saving actor transaction state: %s"

	// Secret.
	ErrSecretStoreNotConfigured = "secret store is没有配置"
	ErrSecretStoreNotFound      = "failed finding secret store with key %s"
	ErrPermissionDenied         = "access denied by policy to get %q from %q"
	ErrSecretGet                = "failed getting secret with key %s from secret store %s: %s"
	ErrBulkSecretGet            = "failed getting secrets from secret store %s: %s"

	// DirectMessaging.
	ErrDirectInvoke         = "调用失败, APP id: %s, err: %s"
	ErrDirectInvokeNoAppID  = "无法从url 或header中获取 dapr-app-id"
	ErrDirectInvokeMethod   = "无效的方法名"
	ErrDirectInvokeNotReady = "调用的API未就绪"

	// Metadata.
	ErrMetadataGet = "失败的反序列化的元数据: %s"

	// Healthz.
	ErrHealthNotReady = "dapr is not ready"

	// Configuration.
	ErrConfigurationStoresNotConfigured = "error configuration stores没有配置"
	ErrConfigurationStoreNotFound       = "error configuration stores %s 没有发现"
	ErrConfigurationGet                 = "fail to get %s from Configuration store %s: %s"
	ErrConfigurationSubscribe           = "fail to subscribe %s from Configuration store %s: %s"
)
