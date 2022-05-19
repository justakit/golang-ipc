package ipc

import "time"

const version = 2 // ipc package version

const (
	minMsgSize            = 1024
	defaultMaxMsgSize     = 3145728 // 3Mb  - Maximum bytes allowed for each message
	defaultSocketBasePath = "/tmp"
	defaultRetryTimer     = 200 * time.Millisecond
)
