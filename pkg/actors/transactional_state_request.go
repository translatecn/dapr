// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package actors

// OperationType 描述一个针对状态存储的CRUD操作。
type OperationType string

const Upsert OperationType = "upsert"
const Delete OperationType = "delete"

// TransactionalRequest 描述了一个给定行为体的一组有状态的操作，这些操作以事务性方式执行。
type TransactionalRequest struct {
	Operations []TransactionalOperation `json:"operations"`
	ActorType  string
	ActorID    string
}

// TransactionalOperation  是参与一个事务的状态操作的请求对象。
type TransactionalOperation struct {
	Operation OperationType `json:"operation"`
	Request   interface{}   `json:"request"`
}

// TransactionalUpsert 定义了一个用于upsert 操作的键/值对。
type TransactionalUpsert struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

// TransactionalDelete 定义了一个删除操作
type TransactionalDelete struct {
	Key string `json:"key"`
}
