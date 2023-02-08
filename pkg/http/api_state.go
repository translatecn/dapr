package http

import (
	"fmt"
	"github.com/dapr/components-contrib/state"
	state_loader "github.com/dapr/dapr/pkg/components/state"
	"github.com/dapr/dapr/pkg/concurrency"
	"github.com/dapr/dapr/pkg/encryption"
	"github.com/dapr/dapr/pkg/messages"
	jsoniter "github.com/json-iterator/go"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/valyala/fasthttp"
	"strings"
)

//构造状态相关的端点
func (a *api) constructStateEndpoints() []Endpoint {
	return []Endpoint{
		{
			Methods: []string{fasthttp.MethodGet},
			Route:   "state/{storeName}/{key}",
			Version: apiVersionV1,
			Handler: a.onGetState, //获取状态
		},
		{
			Methods: []string{fasthttp.MethodPost, fasthttp.MethodPut},
			Route:   "state/{storeName}",
			Version: apiVersionV1,
			Handler: a.onPostState, // 更新、创建状态; 同一个key只能创建一次
		},
		{
			Methods: []string{fasthttp.MethodDelete},
			Route:   "state/{storeName}/{key}",
			Version: apiVersionV1,
			Handler: a.onDeleteState, // 删除状态
		},
		{
			Methods: []string{fasthttp.MethodPost, fasthttp.MethodPut},
			Route:   "state/{storeName}/bulk",
			Version: apiVersionV1,
			Handler: a.onBulkGetState, // 批量获取状态
		},
		{
			Methods: []string{fasthttp.MethodPost, fasthttp.MethodPut},
			Route:   "state/{storeName}/transaction",
			Version: apiVersionV1,
			Handler: a.onPostStateTransaction, // 事务更新状态
		},
		{
			Methods: []string{fasthttp.MethodPost, fasthttp.MethodPut},
			Route:   "state/{storeName}/query",
			Version: apiVersionV1alpha1,
			Handler: a.onQueryState, // 查询状态 todo 存在的意义  与onGetState 的区别
		},
	}
}

// etagError 检查来自状态存储的错误是否是etag错误，并返回一个bool作为指示，一个状态代码和一个错误信息。
func (a *api) etagError(err error) (bool, int, string) {
	e, ok := err.(*state.ETagError)
	if !ok {
		return false, -1, ""
	}
	switch e.Kind() {
	case state.ETagMismatch:
		return true, fasthttp.StatusConflict, e.Error()
	case state.ETagInvalid:
		return true, fasthttp.StatusBadRequest, e.Error()
	}

	return false, -1, ""
}

func (a *api) getStateStoreName(reqCtx *fasthttp.RequestCtx) string {
	return reqCtx.UserValue(storeNameParam).(string)
}

//ok
func (a *api) onDeleteState(reqCtx *fasthttp.RequestCtx) {
	store, storeName, err := a.getStateStoreWithRequestValidation(reqCtx)
	if err != nil {
		log.Debug(err)
		return
	}

	key := reqCtx.UserValue(stateKeyParam).(string)
	//并发性
	//一致性
	concurrency := string(reqCtx.QueryArgs().Peek(concurrencyParam))
	consistency := string(reqCtx.QueryArgs().Peek(consistencyParam))

	metadata := getMetadataFromRequest(reqCtx)
	k, err := state_loader.GetModifiedStateKey(key, storeName, a.id)
	if err != nil {
		msg := NewErrorResponse("ERR_MALFORMED_REQUEST", err.Error())
		respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
		log.Debug(err)
		return
	}
	req := state.DeleteRequest{
		Key: k,
		Options: state.DeleteStateOption{
			Concurrency: concurrency,
			Consistency: consistency,
		},
		Metadata: metadata,
	}
	// 从header中的If-Match 获取
	exists, etag := extractEtag(reqCtx)
	if exists {
		req.ETag = &etag
	}

	err = store.Delete(&req)
	if err != nil {
		statusCode, errMsg, resp := a.stateErrorResponse(err, "ERR_STATE_DELETE") // 从err中解析出状态码，消息，
		resp.Message = fmt.Sprintf(messages.ErrStateDelete, key, errMsg)

		respond(reqCtx, withError(statusCode, resp))
		log.Debug(resp.Message)
		return
	}
	respond(reqCtx, withEmpty())
}

// 创建状态
func (a *api) onPostState(reqCtx *fasthttp.RequestCtx) {
	store, storeName, err := a.getStateStoreWithRequestValidation(reqCtx)
	if err != nil {
		log.Debug(err)
		return
	}

	var reqs []state.SetRequest
	err = a.json.Unmarshal(reqCtx.PostBody(), &reqs)
	if err != nil {
		msg := NewErrorResponse("ERR_MALFORMED_REQUEST", err.Error())
		respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
		log.Debug(msg)
		return
	}
	if len(reqs) == 0 {
		respond(reqCtx, withEmpty())
		return
	}

	for i, r := range reqs {
		reqs[i].Key, err = state_loader.GetModifiedStateKey(r.Key, storeName, a.id) // 覆盖原来的key
		if err != nil {
			msg := NewErrorResponse("ERR_MALFORMED_REQUEST", err.Error())
			respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
			log.Debug(err)
			return
		}
		// 判断存储实例 是否支持加密存储
		if encryption.EncryptedStateStore(storeName) {
			data := []byte(fmt.Sprintf("%v", r.Value))
			val, encErr := encryption.TryEncryptValue(storeName, data)
			if encErr != nil {
				statusCode, errMsg, resp := a.stateErrorResponse(encErr, "ERR_STATE_SAVE")
				resp.Message = fmt.Sprintf(messages.ErrStateSave, storeName, errMsg)

				respond(reqCtx, withError(statusCode, resp))
				log.Debug(resp.Message)
				return
			}

			reqs[i].Value = val
		}
	}

	err = store.BulkSet(reqs)
	if err != nil {
		storeName := a.getStateStoreName(reqCtx)

		statusCode, errMsg, resp := a.stateErrorResponse(err, "ERR_STATE_SAVE")
		resp.Message = fmt.Sprintf(messages.ErrStateSave, storeName, errMsg)

		respond(reqCtx, withError(statusCode, resp))
		log.Debug(resp.Message)
		return
	}

	respond(reqCtx, withEmpty())
}

//ok
func (a *api) onGetState(reqCtx *fasthttp.RequestCtx) {
	store, storeName, err := a.getStateStoreWithRequestValidation(reqCtx)
	if err != nil {
		log.Debug(err)
		return
	}
	// 获取url query param 中的数据
	metadata := getMetadataFromRequest(reqCtx)

	key := reqCtx.UserValue(stateKeyParam).(string)                  // 获取查询的key
	consistency := string(reqCtx.QueryArgs().Peek(consistencyParam)) // 一致性
	k, err := state_loader.GetModifiedStateKey(key, storeName, a.id) // 应用ID
	if err != nil {
		msg := NewErrorResponse("ERR_MALFORMED_REQUEST", fmt.Sprintf(messages.ErrMalformedRequest, err))
		respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
		log.Debug(err)
		return
	}
	req := state.GetRequest{
		Key: k,
		Options: state.GetStateOption{
			Consistency: consistency,
		},
		Metadata: metadata,
	}

	resp, err := store.Get(&req)
	if err != nil {
		msg := NewErrorResponse("ERR_STATE_GET", fmt.Sprintf(messages.ErrStateGet, key, storeName, err.Error()))
		respond(reqCtx, withError(fasthttp.StatusInternalServerError, msg))
		log.Debug(msg)
		return
	}
	if resp == nil || resp.Data == nil {
		respond(reqCtx, withEmpty())
		return
	}

	if encryption.EncryptedStateStore(storeName) {
		val, err := encryption.TryDecryptValue(storeName, resp.Data)
		if err != nil {
			msg := NewErrorResponse("ERR_STATE_GET", fmt.Sprintf(messages.ErrStateGet, key, storeName, err.Error()))
			respond(reqCtx, withError(fasthttp.StatusInternalServerError, msg))
			log.Debug(msg)
			return
		}

		resp.Data = val
	}

	respond(reqCtx, withJSON(fasthttp.StatusOK, resp.Data), withEtag(resp.ETag), withMetadata(resp.Metadata))
}

// ok
func (a *api) onBulkGetState(reqCtx *fasthttp.RequestCtx) {
	store, storeName, err := a.getStateStoreWithRequestValidation(reqCtx)
	if err != nil {
		log.Debug(err)
		return
	}

	var req BulkGetRequest
	err = a.json.Unmarshal(reqCtx.PostBody(), &req)
	if err != nil {
		msg := NewErrorResponse("ERR_MALFORMED_REQUEST", fmt.Sprintf(messages.ErrMalformedRequest, err))
		respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
		log.Debug(msg)
		return
	}

	metadata := getMetadataFromRequest(reqCtx)
	// 返回结果
	bulkResp := make([]BulkGetResponse, len(req.Keys))
	if len(req.Keys) == 0 {
		b, _ := a.json.Marshal(bulkResp)
		respond(reqCtx, withJSON(fasthttp.StatusOK, b))
		return
	}

	// 先试着批量获取
	reqs := make([]state.GetRequest, len(req.Keys))
	// 获取修改后的key
	for i, k := range req.Keys {
		key, err1 := state_loader.GetModifiedStateKey(k, storeName, a.id)
		if err1 != nil {
			msg := NewErrorResponse("ERR_MALFORMED_REQUEST", fmt.Sprintf(messages.ErrMalformedRequest, err1))
			respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
			log.Debug(err1)
			return
		}
		r := state.GetRequest{
			Key:      key,
			Metadata: req.Metadata,
		}
		reqs[i] = r
	}
	bulkGet, responses, err := store.BulkGet(reqs) // bulkGet 判断是不是批量获取的

	if bulkGet {
		// 如果store支持批量获取
		if err != nil {
			msg := NewErrorResponse("ERR_MALFORMED_REQUEST", fmt.Sprintf(messages.ErrMalformedRequest, err))
			respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
			log.Debug(msg)
			return
		}

		for i := 0; i < len(responses) && i < len(req.Keys); i++ {
			// 清理dapr设置的前缀
			bulkResp[i].Key = state_loader.GetOriginalStateKey(responses[i].Key)
			if responses[i].Error != "" {
				log.Debugf("批量获取：获取key错误 %s: %s", bulkResp[i].Key, responses[i].Error)
				bulkResp[i].Error = responses[i].Error
			} else {
				bulkResp[i].Data = jsoniter.RawMessage(responses[i].Data) // 类型转换
				bulkResp[i].ETag = responses[i].ETag
				bulkResp[i].Metadata = responses[i].Metadata
			}
		}
	} else {
		// 如果store不支持批量获取，则退而求其次，逐一调用get()方法。
		limiter := concurrency.NewLimiter(req.Parallelism) // 并发器

		for i, k := range req.Keys {
			bulkResp[i].Key = k

			fn := func(param interface{}) {
				r := param.(*BulkGetResponse)
				// 获取修改后的键值对
				k, err := state_loader.GetModifiedStateKey(r.Key, storeName, a.id)
				if err != nil {
					log.Debug(err)
					r.Error = err.Error()
					return
				}
				gr := &state.GetRequest{
					Key:      k,
					Metadata: metadata,
				}

				resp, err := store.Get(gr)
				if err != nil {
					log.Debugf("批量获取：获取key错误  %s: %s", r.Key, err)
					r.Error = err.Error()
				} else if resp != nil {
					r.Data = jsoniter.RawMessage(resp.Data)
					r.ETag = resp.ETag
					r.Metadata = resp.Metadata
				}
			}

			limiter.Execute(fn, &bulkResp[i])
		}
		limiter.Wait()
	}

	if encryption.EncryptedStateStore(storeName) {
		for i := range bulkResp {
			val, err := encryption.TryDecryptValue(storeName, bulkResp[i].Data)
			if err != nil {
				log.Debugf("批量获取错误: %s", err)
				bulkResp[i].Error = err.Error()
				continue
			}

			bulkResp[i].Data = val
		}
	}

	b, _ := a.json.Marshal(bulkResp)
	respond(reqCtx, withJSON(fasthttp.StatusOK, b))
}

// 获取 存储实例、以及实例名称
func (a *api) getStateStoreWithRequestValidation(reqCtx *fasthttp.RequestCtx) (state.Store, string, error) {
	// 必须已应有组件声明
	if a.stateStores == nil || len(a.stateStores) == 0 {
		msg := NewErrorResponse("ERR_STATE_STORES_NOT_CONFIGURED", messages.ErrStateStoresNotConfigured)
		respond(reqCtx, withError(fasthttp.StatusInternalServerError, msg))
		log.Debug(msg)
		return nil, "", errors.New(msg.Message)
	}
	//http://localhost:3500/v1.0/state/redis-statestore/b 获取状态存储的名字  pkg/http/api.go:197
	storeName := a.getStateStoreName(reqCtx)
	// 调用的组件必须存在
	if a.stateStores[storeName] == nil {
		msg := NewErrorResponse("ERR_STATE_STORE_NOT_FOUND", fmt.Sprintf(messages.ErrStateStoreNotFound, storeName))
		respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
		log.Debug(msg)
		return nil, "", errors.New(msg.Message)
	}
	return a.stateStores[storeName], storeName, nil
}

//提取Etag ,从header中的If-Match 获取
func extractEtag(reqCtx *fasthttp.RequestCtx) (bool, string) {
	var etag string
	var hasEtag bool
	reqCtx.Request.Header.VisitAll(func(key []byte, value []byte) {
		if string(key) == "If-Match" {
			etag = string(value)
			hasEtag = true
			return
		}
	})

	return hasEtag, etag
}

// 获取 请求的URL参数中以metadata.开头的
func getMetadataFromRequest(reqCtx *fasthttp.RequestCtx) map[string]string {
	metadata := map[string]string{}
	reqCtx.QueryArgs().VisitAll(func(key []byte, value []byte) {
		queryKey := string(key)
		if strings.HasPrefix(queryKey, metadataPrefix) {
			k := strings.TrimPrefix(queryKey, metadataPrefix)
			metadata[k] = string(value)
		}
	})

	return metadata
}

//ok
func (a *api) onPostStateTransaction(reqCtx *fasthttp.RequestCtx) {
	if a.stateStores == nil || len(a.stateStores) == 0 {
		msg := NewErrorResponse("ERR_STATE_STORES_NOT_CONFIGURED", messages.ErrStateStoresNotConfigured)
		respond(reqCtx, withError(fasthttp.StatusInternalServerError, msg))
		log.Debug(msg)
		return
	}

	storeName := reqCtx.UserValue(storeNameParam).(string)
	_, ok := a.stateStores[storeName]
	if !ok {
		msg := NewErrorResponse("ERR_STATE_STORE_NOT_FOUND", fmt.Sprintf(messages.ErrStateStoreNotFound, storeName))
		respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
		log.Debug(msg)
		return
	}
	//transactionalStateStores  ==  stateStores
	transactionalStore, ok := a.transactionalStateStores[storeName]
	if !ok {
		msg := NewErrorResponse("ERR_STATE_STORE_NOT_SUPPORTED", fmt.Sprintf(messages.ErrStateStoreNotSupported, storeName))
		respond(reqCtx, withError(fasthttp.StatusInternalServerError, msg))
		log.Debug(msg)
		return
	}

	body := reqCtx.PostBody()
	var req state.TransactionalStateRequest
	if err := a.json.Unmarshal(body, &req); err != nil {
		msg := NewErrorResponse("ERR_MALFORMED_REQUEST", fmt.Sprintf(messages.ErrMalformedRequest, err.Error()))
		respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
		log.Debug(msg)
		return
	}
	if len(req.Operations) == 0 {
		respond(reqCtx, withEmpty())
		return
	}

	var operations []state.TransactionalStateOperation
	for _, o := range req.Operations {
		switch o.Operation {
		case state.Upsert:
			var upsertReq state.SetRequest
			err := mapstructure.Decode(o.Request, &upsertReq)
			if err != nil {
				msg := NewErrorResponse("ERR_MALFORMED_REQUEST", fmt.Sprintf(messages.ErrMalformedRequest, err.Error()))
				respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
				log.Debug(msg)
				return
			}
			upsertReq.Key, err = state_loader.GetModifiedStateKey(upsertReq.Key, storeName, a.id)
			if err != nil {
				msg := NewErrorResponse("ERR_MALFORMED_REQUEST", err.Error())
				respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
				log.Debug(err)
				return
			}
			operations = append(operations, state.TransactionalStateOperation{
				Request:   upsertReq,
				Operation: state.Upsert,
			})
		case state.Delete:
			var delReq state.DeleteRequest
			err := mapstructure.Decode(o.Request, &delReq)
			if err != nil {
				msg := NewErrorResponse("ERR_MALFORMED_REQUEST",
					fmt.Sprintf(messages.ErrMalformedRequest, err.Error()))
				respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
				log.Debug(msg)
				return
			}
			delReq.Key, err = state_loader.GetModifiedStateKey(delReq.Key, storeName, a.id)
			if err != nil {
				msg := NewErrorResponse("ERR_MALFORMED_REQUEST", err.Error())
				respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
				log.Debug(msg)
				return
			}
			operations = append(operations, state.TransactionalStateOperation{
				Request:   delReq,
				Operation: state.Delete,
			})
		default:
			msg := NewErrorResponse("ERR_NOT_SUPPORTED_STATE_OPERATION",
				fmt.Sprintf(messages.ErrNotSupportedStateOperation, o.Operation))
			respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
			log.Debug(msg)
			return
		}
	}

	if encryption.EncryptedStateStore(storeName) {
		for i, op := range operations {
			if op.Operation == state.Upsert {
				req := op.Request.(*state.SetRequest)
				data := []byte(fmt.Sprintf("%v", req.Value))
				val, err := encryption.TryEncryptValue(storeName, data)
				if err != nil {
					msg := NewErrorResponse(
						"ERR_SAVE_STATE",
						fmt.Sprintf(messages.ErrStateSave, storeName, err.Error()))
					respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
					log.Debug(msg)
					return
				}

				req.Value = val
				operations[i].Request = req
			}
		}
	}

	err := transactionalStore.Multi(&state.TransactionalStateRequest{
		Operations: operations,
		Metadata:   req.Metadata,
	})

	if err != nil {
		msg := NewErrorResponse("ERR_STATE_TRANSACTION", fmt.Sprintf(messages.ErrStateTransaction, err.Error()))
		respond(reqCtx, withError(fasthttp.StatusInternalServerError, msg))
		log.Debug(msg)
	} else {
		respond(reqCtx, withEmpty())
	}
}

// ok
func (a *api) onQueryState(reqCtx *fasthttp.RequestCtx) {
	store, storeName, err := a.getStateStoreWithRequestValidation(reqCtx)
	if err != nil {
		// error has been already logged
		return
	}
	// 判断该接口是否实现了query
	querier, ok := store.(state.Querier)
	if !ok {
		msg := NewErrorResponse("ERR_METHOD_NOT_FOUND", fmt.Sprintf(messages.ErrNotFound, "Query"))
		respond(reqCtx, withError(fasthttp.StatusNotFound, msg))
		log.Debug(msg)
		return
	}

	var req state.QueryRequest
	if err = a.json.Unmarshal(reqCtx.PostBody(), &req); err != nil {
		msg := NewErrorResponse("ERR_MALFORMED_REQUEST", fmt.Sprintf(messages.ErrMalformedRequest, err.Error()))
		respond(reqCtx, withError(fasthttp.StatusBadRequest, msg))
		log.Debug(msg)
		return
	}
	req.Metadata = getMetadataFromRequest(reqCtx)

	resp, err := querier.Query(&req)
	if err != nil {
		msg := NewErrorResponse("ERR_STATE_QUERY", fmt.Sprintf(messages.ErrStateQuery, storeName, err.Error()))
		respond(reqCtx, withError(fasthttp.StatusInternalServerError, msg))
		log.Debug(msg)
		return
	}
	if resp == nil || len(resp.Results) == 0 {
		respond(reqCtx, withEmpty())
		return
	}

	encrypted := encryption.EncryptedStateStore(storeName)

	qresp := QueryResponse{
		Results:  make([]QueryItem, len(resp.Results)),
		Token:    resp.Token,
		Metadata: resp.Metadata,
	}
	for i := range resp.Results {
		qresp.Results[i].Key = state_loader.GetOriginalStateKey(resp.Results[i].Key)
		qresp.Results[i].ETag = resp.Results[i].ETag
		qresp.Results[i].Error = resp.Results[i].Error
		if encrypted {
			val, err := encryption.TryDecryptValue(storeName, resp.Results[i].Data)
			if err != nil {
				log.Debugf("query error: %s", err)
				qresp.Results[i].Error = err.Error()
				continue
			}
			qresp.Results[i].Data = jsoniter.RawMessage(val)
		} else {
			qresp.Results[i].Data = jsoniter.RawMessage(resp.Results[i].Data)
		}
	}

	b, _ := a.json.Marshal(qresp)
	respond(reqCtx, withJSON(fasthttp.StatusOK, b))
}
