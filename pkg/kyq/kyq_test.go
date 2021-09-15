// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package kyq

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ktestutil "sigs.k8s.io/cli-utils/pkg/kstatus/polling/testutil"
)

var o1y = `
apiVersion: v1
kind: Pod
metadata:
  name: pod-name
  namespace: pod-namespace
list:
- 1
- b
- false
map:
  a:
  - "1"
  - "2"
  - "3"
  b: null
  c:
  - x
  - ?: null
  - z
`

var o2y = `
apiVersion: v1
kind: Pod
metadata:
  name: pod-name
  namespace: pod-namespace
`

func TestGet(t *testing.T) {
	o1 := ktestutil.YamlToUnstructured(t, o1y)

	testCases := map[string]struct {
		obj           *unstructured.Unstructured
		path          string
		value         interface{}
		expectedError error
	}{
		"string": {
			obj:   o1,
			path:  ".kind",
			value: "Pod",
		},
		"string in map": {
			obj:   o1,
			path:  ".metadata.name",
			value: "pod-name",
		},
		"int in array": {
			obj:   o1,
			path:  ".list[0]",
			value: 1,
		},
		"string in array": {
			obj:   o1,
			path:  ".list[1]",
			value: "b",
		},
		"bool in array": {
			obj:   o1,
			path:  ".list[2]",
			value: false,
		},
		"string in array in map": {
			obj:   o1,
			path:  ".map.c[2]",
			value: "z",
		},
		"nil in map in array in map": {
			obj:   o1,
			path:  ".map.c[1][\"?\"]",
			value: nil,
		},
		"array in map": {
			obj:   o1,
			path:  ".map.a",
			value: []interface{}{"1", "2", "3"},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			value, found, err := Get(tc.obj.Object, tc.path)
			require.NoError(t, err)
			require.True(t, found)

			if reflect.TypeOf(tc.value) != reflect.TypeOf(value) {
				t.Errorf("unexpected value type: expected (%T), received (%T)", tc.value, value)
			}

			if !assert.ObjectsAreEqual(tc.value, value) {
				t.Errorf("unexpected value: expected (%#v), received (%#v)", tc.value, value)
			}
		})
	}
}

func TestSet(t *testing.T) {
	o2 := ktestutil.YamlToUnstructured(t, o2y)

	testCases := map[string]struct {
		obj           *unstructured.Unstructured
		path          string
		value         interface{}
		expectedError error
	}{
		"string": {
			obj:   o2,
			path:  ".kind",
			value: "Pod",
		},
		"string in map": {
			obj:   o2,
			path:  ".metadata.name",
			value: "pod-name",
		},
		"int in array": {
			obj:   o2,
			path:  ".list[0]",
			value: 1,
		},
		"string in array": {
			obj:   o2,
			path:  ".list[1]",
			value: "b",
		},
		"bool in array": {
			obj:   o2,
			path:  ".list[2]",
			value: false,
		},
		"string in array in map": {
			obj:   o2,
			path:  ".map.c[2]",
			value: "z",
		},
		"nil in map in array in map": {
			obj:   o2,
			path:  ".map.c[1][\"?\"]",
			value: nil,
		},
		"array in map": {
			obj:   o2,
			path:  ".map.a",
			value: []interface{}{"1", "2", "3"},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			err := Set(tc.obj.Object, tc.path, tc.value)
			require.NoError(t, err)

			value, found, err := Get(tc.obj.Object, tc.path)
			require.NoError(t, err)
			require.True(t, found)

			if reflect.TypeOf(tc.value) != reflect.TypeOf(value) {
				t.Errorf("unexpected value type: expected (%T), received (%T)", tc.value, value)
			}

			if !assert.ObjectsAreEqual(tc.value, value) {
				t.Errorf("unexpected value: expected (%#v), received (%#v)", tc.value, value)
			}
		})
	}
}
