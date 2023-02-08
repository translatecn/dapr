package scopes

import (
	"strings"
)

const (
	SubscriptionScopes = "subscriptionScopes"
	PublishingScopes   = "publishingScopes"
	AllowedTopics      = "allowedTopics"
	appsSeparator      = ";"
	appSeparator       = "="
	topicSeparator     = ","
)


// GetScopedTopics 从Pub/Sub组件属性中返回一个给定应用程序的范围内主题的列表。
//"dp-61c2cb20562850d49d47d1c7-executorapp=a,b,c,d;"
func GetScopedTopics(scope, appID string, metadata map[string]string) []string {
	var (
		existM = map[string]struct{}{}
		topics []string
	)

	if val, ok := metadata[scope]; ok && val != "" {
		val = strings.ReplaceAll(val, " ", "")
		apps := strings.Split(val, appsSeparator)
		for _, a := range apps {
			appTopics := strings.Split(a, appSeparator)
			if len(appTopics) == 0 {
				continue
			}

			app := appTopics[0]
			if app != appID {
				continue
			}

			tempTopics := strings.Split(appTopics[1], topicSeparator)
			for _, tempTopic := range tempTopics {
				if _, ok = existM[tempTopic]; !ok {
					existM[tempTopic] = struct{}{}
					topics = append(topics, tempTopic)
				}
			}
		}
	}
	return topics
}

// GetAllowedTopics 返回params allowedTopics的所有主题列表。
func GetAllowedTopics(metadata map[string]string) []string {
	var (
		existM = map[string]struct{}{}
		topics []string
	)

	if val, ok := metadata[AllowedTopics]; ok && val != "" {
		val = strings.ReplaceAll(val, " ", "")
		tempTopics := strings.Split(val, topicSeparator)
		for _, tempTopic := range tempTopics {
			if _, ok = existM[tempTopic]; !ok {
				existM[tempTopic] = struct{}{}
				topics = append(topics, tempTopic)
			}
		}
	}
	return topics
}
