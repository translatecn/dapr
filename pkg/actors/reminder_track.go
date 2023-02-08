// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package actors

// ReminderTrack 是一个持久化的对象，它记录了最后一次提醒的时间。
type ReminderTrack struct {
	LastFiredTime  string `json:"lastFiredTime"`
	RepetitionLeft int    `json:"repetitionLeft"`
}
