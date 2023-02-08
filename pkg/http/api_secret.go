package http

import (
	"fmt"
	"github.com/dapr/components-contrib/secretstores"
	"github.com/dapr/dapr/pkg/messages"
	"github.com/pkg/errors"
	"github.com/valyala/fasthttp"
)

//构造secret相关的端点
func (a *api) constructSecretEndpoints() []Endpoint {
	return []Endpoint{
		{
			Methods: []string{fasthttp.MethodGet},
			Route:   "secrets/{secretStoreName}/bulk",
			Version: apiVersionV1,
			Handler: a.onBulkGetSecret, // 批量获取
		},
		{
			Methods: []string{fasthttp.MethodGet},
			Route:   "secrets/{secretStoreName}/{key}",
			Version: apiVersionV1,
			Handler: a.onGetSecret, // 只获取一个
		},
	}
}

func (a *api) onGetSecret(reqCtx *fasthttp.RequestCtx) {
	store, secretStoreName, err := a.getSecretStoreWithRequestValidation(reqCtx)
	if err != nil {
		log.Debug(err)
		return
	}

	metadata := getMetadataFromRequest(reqCtx)

	key := reqCtx.UserValue(secretNameParam).(string)

	if !a.isSecretAllowed(secretStoreName, key) {
		msg := NewErrorResponse("ERR_PERMISSION_DENIED", fmt.Sprintf(messages.ErrPermissionDenied, key, secretStoreName))
		respond(reqCtx, withError(fasthttp.StatusForbidden, msg))
		return
	}

	req := secretstores.GetSecretRequest{
		Name:     key,
		Metadata: metadata,
	}

	resp, err := store.GetSecret(req)
	if err != nil {
		msg := NewErrorResponse("ERR_SECRET_GET",
			fmt.Sprintf(messages.ErrSecretGet, req.Name, secretStoreName, err.Error()))
		respond(reqCtx, withError(fasthttp.StatusInternalServerError, msg))
		log.Debug(msg)
		return
	}

	if resp.Data == nil {
		respond(reqCtx, withEmpty())
		return
	}

	respBytes, _ := a.json.Marshal(resp.Data)
	respond(reqCtx, withJSON(fasthttp.StatusOK, respBytes))
}

func (a *api) onBulkGetSecret(reqCtx *fasthttp.RequestCtx) {
	store, secretStoreName, err := a.getSecretStoreWithRequestValidation(reqCtx)
	if err != nil {
		log.Debug(err)
		return
	}

	metadata := getMetadataFromRequest(reqCtx)

	req := secretstores.BulkGetSecretRequest{
		Metadata: metadata,
	}

	resp, err := store.BulkGetSecret(req)
	if err != nil {
		msg := NewErrorResponse("ERR_SECRET_GET",
			fmt.Sprintf(messages.ErrBulkSecretGet, secretStoreName, err.Error()))
		respond(reqCtx, withError(fasthttp.StatusInternalServerError, msg))
		log.Debug(msg)
		return
	}

	if resp.Data == nil {
		respond(reqCtx, withEmpty())
		return
	}

	filteredSecrets := map[string]map[string]string{}
	for key, v := range resp.Data {
		if a.isSecretAllowed(secretStoreName, key) {
			filteredSecrets[key] = v
		} else {
			log.Debugf(messages.ErrPermissionDenied, key, secretStoreName)
		}
	}

	respBytes, _ := a.json.Marshal(filteredSecrets)
	respond(reqCtx, withJSON(fasthttp.StatusOK, respBytes))
}

func (a *api) getSecretStoreWithRequestValidation(reqCtx *fasthttp.RequestCtx) (secretstores.SecretStore, string, error) {
	if a.secretStores == nil || len(a.secretStores) == 0 {
		msg := NewErrorResponse("ERR_SECRET_STORES_NOT_CONFIGURED", messages.ErrSecretStoreNotConfigured)
		respond(reqCtx, withError(fasthttp.StatusInternalServerError, msg))
		return nil, "", errors.New(msg.Message)
	}

	secretStoreName := reqCtx.UserValue(secretStoreNameParam).(string)

	if a.secretStores[secretStoreName] == nil {
		msg := NewErrorResponse("ERR_SECRET_STORE_NOT_FOUND", fmt.Sprintf(messages.ErrSecretStoreNotFound, secretStoreName))
		respond(reqCtx, withError(fasthttp.StatusUnauthorized, msg))
		return nil, "", errors.New(msg.Message)
	}
	return a.secretStores[secretStoreName], secretStoreName, nil
}
