package ews

import "time"

type WatchOptions struct {
	SubscribeToAllFolders bool
	FolderIDs             []string
	ConnectionTimeout     time.Duration
}

type StreamEvent struct {
	Type              string
	ItemID            string
	OldItemID         string
	FolderID          string
	ParentFolderID    string
	OldParentFolderID string
	Watermark         string
}

type StreamBatch struct {
	SubscriptionID    string
	PreviousWatermark string
	MoreEvents        bool
	Events            []StreamEvent
}
