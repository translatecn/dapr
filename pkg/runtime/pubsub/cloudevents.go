// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package pubsub

import (
	"github.com/google/uuid"

	contrib_contenttype "github.com/dapr/components-contrib/contenttype"
	contrib_pubsub "github.com/dapr/components-contrib/pubsub"
)

// CloudEvent 是一个请求对象，用来创建一个兼容Dapr的cloudevent。
type CloudEvent struct {
	ID              string
	Data            []byte
	Topic           string
	Pubsub          string
	DataContentType string
	TraceID         string
}

// NewCloudEvent 封装了从现有的云事件或原始负载创建Dapr云事件的过程。
func NewCloudEvent(req *CloudEvent) (map[string]interface{}, error) {
	// application/cloudevents+json
	if contrib_contenttype.IsCloudEventContentType(req.DataContentType) {
		return contrib_pubsub.FromCloudEvent(req.Data, req.Topic, req.Pubsub, req.TraceID)
	}
	return contrib_pubsub.NewCloudEventsEnvelope(uuid.New().String(), req.ID, contrib_pubsub.DefaultCloudEventType, "", req.Topic, req.Pubsub,
		req.DataContentType, req.Data, req.TraceID), nil
}
