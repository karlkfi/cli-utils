// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package mutator

import (
	"os"
	"testing"

	"sigs.k8s.io/cli-utils/pkg/logutil"

	logging "gopkg.in/op/go-logging.v1"
	"k8s.io/klog/v2"
)

// TestMain executes the tests for this package.
// This impl also routes go-logging logs through klog.
// To see all logs, use:
// go test sigs.k8s.io/cli-utils/pkg/kyq -v -args -v=5
func TestMain(m *testing.M) {
	klog.InitFlags(nil)
	logging.SetBackend(logutil.NewKlogGoLoggingBackend())
	os.Exit(m.Run())
}
