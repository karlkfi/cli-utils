// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package logutil

import (
	logging "gopkg.in/op/go-logging.v1"
	"k8s.io/klog/v2"
)

// LogFunc is an interface for klog's global <level>Depth functions.
type LogFunc func(calldepth int, args ...interface{})

// KlogGoLoggingBackend is a backend for gopkg.in/op/go-logging.v1 that
// delegates to k8s.io/klog/v2. To use, call:
// `logging.SetBackend(KlogGoLoggingBackend{})`.
// This is included in cli-utils because certain dependencies
// (e.g. github.com/mikefarah/yq) use go-logging, while cli-utils and other
// Kubernetes code uses klog.
type KlogGoLoggingBackend struct {
	// map of go-logging level to klog verbosity level
	LevelVerbosity map[logging.Level]klog.Level
	LevelLog       map[logging.Level]LogFunc
}

// NewKlogGoLoggingBackend returns a KlogGoLoggingBackend instance with default
// mappings for LevelVerbosity and LevelLog.
// Level mappings based on Kubernetes logging conventions:
// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-instrumentation/logging.md
func NewKlogGoLoggingBackend() *KlogGoLoggingBackend {
	return &KlogGoLoggingBackend{
		LevelVerbosity: map[logging.Level]klog.Level{
			logging.CRITICAL: klog.Level(0),
			logging.ERROR:    klog.Level(0),
			logging.WARNING:  klog.Level(0),
			logging.NOTICE:   klog.Level(1),
			logging.INFO:     klog.Level(2),
			logging.DEBUG:    klog.Level(5),
		},
		LevelLog: map[logging.Level]LogFunc{
			logging.CRITICAL: klog.ErrorDepth,
			logging.ERROR:    klog.ErrorDepth,
			logging.WARNING:  klog.WarningDepth,
			logging.NOTICE:   klog.InfoDepth,
			logging.INFO:     klog.InfoDepth,
			logging.DEBUG:    klog.InfoDepth,
		},
	}
}

// Log sends the go-logging message to klog using LevelVerbosity to determine
// verbosity level (0-5) and LevelLog to determine log function (error, warning,
// info).
func (k KlogGoLoggingBackend) Log(level logging.Level, calldepth int, rec *logging.Record) error {
	v, ok := k.LevelVerbosity[level]
	if !ok {
		// default verbosity to 0 (always visible)
		v = 0
	}
	if !klog.V(v).Enabled() {
		return nil
	}
	logFunc, ok := k.LevelLog[level]
	if !ok {
		// default log to error
		logFunc = klog.ErrorDepth
	}
	logFunc(calldepth+1, rec.Message())
	return nil
}
