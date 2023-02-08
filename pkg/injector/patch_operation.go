// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package injector

// PatchOperation 表示要应用于Kubernetes资源的谨慎更改。
type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// 		{
//            "op": "add",
//            "path": "/spec/containers/-", 固定写法
//            "value": {
//                "name": "flask-injector",
//                "image": "centos:7",
//                "command": ["ping", '127.0.0.1'],
//                "args": [],
//                "ports": [],
//                "env": [],
//                "resources": {},
//            }
//        } 修改后的数据demo
