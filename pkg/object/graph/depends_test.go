// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/cli-utils/pkg/object/mutation"
	mutationutil "sigs.k8s.io/cli-utils/pkg/object/mutation/testutil"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

var (
	resources = map[string]string{
		"pod": `
kind: Pod
apiVersion: v1
metadata:
  name: test-pod
  namespace: test-namespace
`,
		"default-pod": `
kind: Pod
apiVersion: v1
metadata:
  name: pod-in-default-namespace
  namespace: default
`,
		"deployment": `
kind: Deployment
apiVersion: apps/v1
metadata:
  name: foo
  namespace: test-namespace
  uid: dep-uid
  generation: 1
spec:
  replicas: 1
`,
		"secret": `
kind: Secret
apiVersion: v1
metadata:
  name: secret
  namespace: test-namespace
  uid: secret-uid
  generation: 1
type: Opaque
spec:
  foo: bar
`,
		"namespace": `
kind: Namespace
apiVersion: v1
metadata:
  name: test-namespace
`,

		"crd": `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: crontabs.stable.example.com
spec:
  group: stable.example.com
  versions:
    - name: v1
      served: true
      storage: true
  scope: Namespaced
  names:
    plural: crontabs
    singular: crontab
    kind: CronTab
`,
		"crontab1": `
apiVersion: "stable.example.com/v1"
kind: CronTab
metadata:
  name: cron-tab-01
  namespace: test-namespace
`,
		"crontab2": `
apiVersion: "stable.example.com/v1"
kind: CronTab
metadata:
  name: cron-tab-02
  namespace: test-namespace
`,
		"configmap1": `
apiVersion: v1
kind: ConfigMap
metadata:
  name: map1-name
  namespace: test-namespace
  annotations:
    config.kubernetes.io/depends-on: apps/namespaces/test-namespace/Deployment/foo
    config.kubernetes.io/apply-time-mutation: |
      - sourceRef:
          kind: Pod
          name: test-pod
          namespace: test-namespace
        sourcePath: $.unused
        targetPath: $.unused
data: {}
`,
		"configmap2": `
apiVersion: v1
kind: ConfigMap
metadata:
  name: map2-name
  namespace: test-namespace
  annotations:
    config.kubernetes.io/apply-time-mutation: |
      - sourceRef:
          group: apps
          kind: Deployment
          name: foo
          namespace: test-namespace
        sourcePath: $.unused
        targetPath: $.unused
data: {}
`,
	}
)

func TestSortObjs(t *testing.T) {
	testCases := map[string]struct {
		objs            []*unstructured.Unstructured
		expectedObjSets []object.UnstructuredSet
		expectedDepSets []object.ObjMetadataSet
		isError         bool
	}{
		"no objects returns no object sets": {
			objs:            []*unstructured.Unstructured{},
			expectedObjSets: nil,
			expectedDepSets: nil,
			isError:         false,
		},
		"one object returns single object set": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["deployment"]),
			},
			expectedObjSets: []object.UnstructuredSet{
				{
					testutil.Unstructured(t, resources["deployment"]),
				},
			},
			expectedDepSets: []object.ObjMetadataSet{{}},
			isError:         false,
		},
		"two unrelated objects returns single object set with two objs": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["deployment"]),
				testutil.Unstructured(t, resources["secret"]),
			},
			expectedObjSets: []object.UnstructuredSet{
				{
					testutil.Unstructured(t, resources["deployment"]),
					testutil.Unstructured(t, resources["secret"]),
				},
			},
			expectedDepSets: []object.ObjMetadataSet{{}},
			isError:         false,
		},
		"one object depends on the other; two single object sets": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["deployment"],
					testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["secret"]))),
				testutil.Unstructured(t, resources["secret"]),
			},
			expectedObjSets: []object.UnstructuredSet{
				{
					testutil.Unstructured(t, resources["secret"]),
				},
				{
					testutil.Unstructured(t, resources["deployment"],
						testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["secret"]))),
				},
			},
			expectedDepSets: []object.ObjMetadataSet{{}, {}},
			isError:         false,
		},
		"three objects depend on another; three single object sets": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["deployment"],
					testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["secret"]))),
				testutil.Unstructured(t, resources["secret"],
					testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["pod"]))),
				testutil.Unstructured(t, resources["pod"]),
			},
			expectedObjSets: []object.UnstructuredSet{
				{
					testutil.Unstructured(t, resources["pod"]),
				},
				{
					testutil.Unstructured(t, resources["secret"],
						testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["pod"]))),
				},
				{
					testutil.Unstructured(t, resources["deployment"],
						testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["secret"]))),
				},
			},
			expectedDepSets: []object.ObjMetadataSet{{}, {}, {}},
			isError:         false,
		},
		"Two objects depend on secret; two object sets": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["deployment"],
					testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["secret"]))),
				testutil.Unstructured(t, resources["pod"],
					testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["secret"]))),
				testutil.Unstructured(t, resources["secret"]),
			},
			expectedObjSets: []object.UnstructuredSet{
				{
					testutil.Unstructured(t, resources["secret"]),
				},
				{
					testutil.Unstructured(t, resources["pod"],
						testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["secret"]))),
					testutil.Unstructured(t, resources["deployment"],
						testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["secret"]))),
				},
			},
			expectedDepSets: []object.ObjMetadataSet{{}, {}},
			isError:         false,
		},
		"two objects applied with their namespace; two object sets": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["deployment"]),
				testutil.Unstructured(t, resources["namespace"]),
				testutil.Unstructured(t, resources["secret"]),
			},
			expectedObjSets: []object.UnstructuredSet{
				{
					testutil.Unstructured(t, resources["namespace"]),
				},
				{
					testutil.Unstructured(t, resources["secret"]),
					testutil.Unstructured(t, resources["deployment"]),
				},
			},
			expectedDepSets: []object.ObjMetadataSet{{}, {}},
			isError:         false,
		},
		"two custom resources applied with their CRD; two object sets": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["crontab1"]),
				testutil.Unstructured(t, resources["crontab2"]),
				testutil.Unstructured(t, resources["crd"]),
			},
			expectedObjSets: []object.UnstructuredSet{
				{
					testutil.Unstructured(t, resources["crd"]),
				},
				{
					testutil.Unstructured(t, resources["crontab1"]),
					testutil.Unstructured(t, resources["crontab2"]),
				},
			},
			expectedDepSets: []object.ObjMetadataSet{{}, {}},
			isError:         false,
		},
		"two custom resources wit CRD and namespace; two object sets": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["crontab1"]),
				testutil.Unstructured(t, resources["crontab2"]),
				testutil.Unstructured(t, resources["namespace"]),
				testutil.Unstructured(t, resources["crd"]),
			},
			expectedObjSets: []object.UnstructuredSet{
				{
					testutil.Unstructured(t, resources["crd"]),
					testutil.Unstructured(t, resources["namespace"]),
				},
				{
					testutil.Unstructured(t, resources["crontab1"]),
					testutil.Unstructured(t, resources["crontab2"]),
				},
			},
			expectedDepSets: []object.ObjMetadataSet{{}, {}},
			isError:         false,
		},
		"two resources with overlapping external dependencies": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["configmap1"]),
				testutil.Unstructured(t, resources["configmap2"]),
			},
			expectedObjSets: []object.UnstructuredSet{
				{},
				{
					testutil.Unstructured(t, resources["configmap1"]),
					testutil.Unstructured(t, resources["configmap2"]),
				},
			},
			expectedDepSets: []object.ObjMetadataSet{
				{
					testutil.ToIdentifier(t, resources["pod"]),
					testutil.ToIdentifier(t, resources["deployment"]), // deduped
				},
				{},
			},
			isError: false,
		},
		"two objects depends on each other is cyclic dependency": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["deployment"],
					testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["secret"]))),
				testutil.Unstructured(t, resources["secret"],
					testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["deployment"]))),
			},
			expectedObjSets: nil,
			expectedDepSets: nil,
			isError:         true,
		},
		"three objects in cyclic dependency": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["deployment"],
					testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["secret"]))),
				testutil.Unstructured(t, resources["secret"],
					testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["pod"]))),
				testutil.Unstructured(t, resources["pod"],
					testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["deployment"]))),
			},
			expectedObjSets: nil,
			expectedDepSets: nil,
			isError:         true,
		},
	}

	for tn, tc := range testCases {
		t.Run(tn, func(t *testing.T) {
			receivedObjSets, receivedDepSets, err := SortObjs(tc.objs)
			if tc.isError {
				assert.NotNil(t, err, "expected error, but received none")
				return
			}
			assert.Nil(t, err, "unexpected error received")
			testutil.AssertEqual(t, receivedObjSets, tc.expectedObjSets)
			testutil.AssertEqual(t, receivedDepSets, tc.expectedDepSets)
		})
	}
}

func TestReverseSortObjs(t *testing.T) {
	testCases := map[string]struct {
		objs            []*unstructured.Unstructured
		expectedObjSets []object.UnstructuredSet
		expectedDepSets []object.ObjMetadataSet
		isError         bool
	}{
		"no objects returns no object sets": {
			objs:            []*unstructured.Unstructured{},
			expectedObjSets: nil,
			expectedDepSets: nil,
			isError:         false,
		},
		"one object returns single object set": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["deployment"]),
			},
			expectedObjSets: []object.UnstructuredSet{
				{
					testutil.Unstructured(t, resources["deployment"]),
				},
			},
			expectedDepSets: []object.ObjMetadataSet{{}},
			isError:         false,
		},
		"three objects depend on another; three single object sets in opposite order": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["deployment"],
					testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["secret"]))),
				testutil.Unstructured(t, resources["secret"],
					testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["pod"]))),
				testutil.Unstructured(t, resources["pod"]),
			},
			expectedObjSets: []object.UnstructuredSet{
				{
					testutil.Unstructured(t, resources["deployment"],
						testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["secret"]))),
				},
				{
					testutil.Unstructured(t, resources["secret"],
						testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["pod"]))),
				},
				{
					testutil.Unstructured(t, resources["pod"]),
				},
			},
			expectedDepSets: []object.ObjMetadataSet{{}, {}, {}},
			isError:         false,
		},
		"two objects applied with their namespace; two sets in opposite order": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["deployment"]),
				testutil.Unstructured(t, resources["namespace"]),
				testutil.Unstructured(t, resources["secret"]),
			},
			expectedObjSets: []object.UnstructuredSet{
				{
					testutil.Unstructured(t, resources["secret"]),
					testutil.Unstructured(t, resources["deployment"]),
				},
				{
					testutil.Unstructured(t, resources["namespace"]),
				},
			},
			expectedDepSets: []object.ObjMetadataSet{{}, {}},
			isError:         false,
		},
		"two custom resources with CRD and namespace; two object sets in opposite order": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["crontab1"]),
				testutil.Unstructured(t, resources["crontab2"]),
				testutil.Unstructured(t, resources["namespace"]),
				testutil.Unstructured(t, resources["crd"]),
			},
			expectedObjSets: []object.UnstructuredSet{
				{
					testutil.Unstructured(t, resources["crontab1"]),
					testutil.Unstructured(t, resources["crontab2"]),
				},
				{
					testutil.Unstructured(t, resources["crd"]),
					testutil.Unstructured(t, resources["namespace"]),
				},
			},
			expectedDepSets: []object.ObjMetadataSet{{}, {}},
			isError:         false,
		},
		"two resources with overlapping external dependencies; two object sets in opposite order": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["configmap1"]),
				testutil.Unstructured(t, resources["configmap2"]),
			},
			expectedObjSets: []object.UnstructuredSet{
				{
					testutil.Unstructured(t, resources["configmap1"]),
					testutil.Unstructured(t, resources["configmap2"]),
				},
				{},
			},
			expectedDepSets: []object.ObjMetadataSet{
				{},
				{
					testutil.ToIdentifier(t, resources["pod"]),
					testutil.ToIdentifier(t, resources["deployment"]), // deduped
				},
			},
			isError: false,
		},
	}

	for tn, tc := range testCases {
		t.Run(tn, func(t *testing.T) {
			receivedObjSets, receivedDepSets, err := ReverseSortObjs(tc.objs)
			if tc.isError {
				assert.NotNil(t, err, "expected error, but received none")
				return
			}
			assert.Nil(t, err, "unexpected error received")
			testutil.AssertEqual(t, receivedObjSets, tc.expectedObjSets)
			testutil.AssertEqual(t, receivedDepSets, tc.expectedDepSets)
		})
	}
}

func TestApplyTimeMutationEdges(t *testing.T) {
	testCases := map[string]struct {
		objs     []*unstructured.Unstructured
		expected []Edge
	}{
		"no objects adds no graph edges": {
			objs:     []*unstructured.Unstructured{},
			expected: []Edge{},
		},
		"no depends-on annotations adds no graph edges": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["deployment"]),
			},
			expected: []Edge{},
		},
		"no depends-on annotations, two objects, adds no graph edges": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["deployment"]),
				testutil.Unstructured(t, resources["secret"]),
			},
			expected: []Edge{},
		},
		"two dependent objects, adds one edge": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(
					t,
					resources["deployment"],
					mutationutil.AddApplyTimeMutation(t, &mutation.ApplyTimeMutation{
						{
							SourceRef:  mutation.NewResourceReference(testutil.Unstructured(t, resources["secret"])),
							SourcePath: "unused",
							TargetPath: "unused",
							Token:      "unused",
						},
					}),
				),
				testutil.Unstructured(t, resources["secret"]),
			},
			expected: []Edge{
				{
					From: testutil.ToIdentifier(t, resources["deployment"]),
					To:   testutil.ToIdentifier(t, resources["secret"]),
				},
			},
		},
		"three dependent objects, adds two edges": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(
					t,
					resources["deployment"],
					mutationutil.AddApplyTimeMutation(t, &mutation.ApplyTimeMutation{
						{
							SourceRef:  mutation.NewResourceReference(testutil.Unstructured(t, resources["secret"])),
							SourcePath: "unused",
							TargetPath: "unused",
							Token:      "unused",
						},
					}),
				),
				testutil.Unstructured(
					t,
					resources["pod"],
					mutationutil.AddApplyTimeMutation(t, &mutation.ApplyTimeMutation{
						{
							SourceRef:  mutation.NewResourceReference(testutil.Unstructured(t, resources["secret"])),
							SourcePath: "unused",
							TargetPath: "unused",
							Token:      "unused",
						},
					}),
				),
				testutil.Unstructured(t, resources["secret"]),
			},
			expected: []Edge{
				{
					From: testutil.ToIdentifier(t, resources["deployment"]),
					To:   testutil.ToIdentifier(t, resources["secret"]),
				},
				{
					From: testutil.ToIdentifier(t, resources["pod"]),
					To:   testutil.ToIdentifier(t, resources["secret"]),
				},
			},
		},
		"pod has two dependencies, adds two edges": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(
					t,
					resources["pod"],
					mutationutil.AddApplyTimeMutation(t, &mutation.ApplyTimeMutation{
						{
							SourceRef:  mutation.NewResourceReference(testutil.Unstructured(t, resources["secret"])),
							SourcePath: "unused",
							TargetPath: "unused",
							Token:      "unused",
						},
						{
							SourceRef:  mutation.NewResourceReference(testutil.Unstructured(t, resources["deployment"])),
							SourcePath: "unused",
							TargetPath: "unused",
							Token:      "unused",
						},
					}),
				),
				testutil.Unstructured(t, resources["deployment"]),
				testutil.Unstructured(t, resources["secret"]),
			},
			expected: []Edge{
				{
					From: testutil.ToIdentifier(t, resources["pod"]),
					To:   testutil.ToIdentifier(t, resources["secret"]),
				},
				{
					From: testutil.ToIdentifier(t, resources["pod"]),
					To:   testutil.ToIdentifier(t, resources["deployment"]),
				},
			},
		},
	}

	for tn, tc := range testCases {
		t.Run(tn, func(t *testing.T) {
			g := New()
			addApplyTimeMutationEdges(g, tc.objs)
			actual := g.GetEdges()
			verifyEdges(t, tc.expected, actual)
		})
	}
}

func TestAddDependsOnEdges(t *testing.T) {
	testCases := map[string]struct {
		objs     []*unstructured.Unstructured
		expected []Edge
	}{
		"no objects adds no graph edges": {
			objs:     []*unstructured.Unstructured{},
			expected: []Edge{},
		},
		"no depends-on annotations adds no graph edges": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["deployment"]),
			},
			expected: []Edge{},
		},
		"no depends-on annotations, two objects, adds no graph edges": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["deployment"]),
				testutil.Unstructured(t, resources["secret"]),
			},
			expected: []Edge{},
		},
		"two dependent objects, adds one edge": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["deployment"],
					testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["secret"]))),
				testutil.Unstructured(t, resources["secret"]),
			},
			expected: []Edge{
				{
					From: testutil.ToIdentifier(t, resources["deployment"]),
					To:   testutil.ToIdentifier(t, resources["secret"]),
				},
			},
		},
		"three dependent objects, adds two edges": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["deployment"],
					testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["secret"]))),
				testutil.Unstructured(t, resources["pod"],
					testutil.AddDependsOn(t, testutil.ToIdentifier(t, resources["secret"]))),
				testutil.Unstructured(t, resources["secret"]),
			},
			expected: []Edge{
				{
					From: testutil.ToIdentifier(t, resources["deployment"]),
					To:   testutil.ToIdentifier(t, resources["secret"]),
				},
				{
					From: testutil.ToIdentifier(t, resources["pod"]),
					To:   testutil.ToIdentifier(t, resources["secret"]),
				},
			},
		},
		"pod has two dependencies, adds two edges": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["pod"],
					testutil.AddDependsOn(t,
						testutil.ToIdentifier(t, resources["secret"]),
						testutil.ToIdentifier(t, resources["deployment"]),
					),
				),
				testutil.Unstructured(t, resources["deployment"]),
				testutil.Unstructured(t, resources["secret"]),
			},
			expected: []Edge{
				{
					From: testutil.ToIdentifier(t, resources["pod"]),
					To:   testutil.ToIdentifier(t, resources["secret"]),
				},
				{
					From: testutil.ToIdentifier(t, resources["pod"]),
					To:   testutil.ToIdentifier(t, resources["deployment"]),
				},
			},
		},
	}

	for tn, tc := range testCases {
		t.Run(tn, func(t *testing.T) {
			g := New()
			addDependsOnEdges(g, tc.objs)
			actual := g.GetEdges()
			verifyEdges(t, tc.expected, actual)
		})
	}
}

func TestAddNamespaceEdges(t *testing.T) {
	testCases := map[string]struct {
		objs     []*unstructured.Unstructured
		expected []Edge
	}{
		"no namespace objects adds no graph edges": {
			objs:     []*unstructured.Unstructured{},
			expected: []Edge{},
		},
		"single namespace adds no graph edges": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["namespace"]),
			},
			expected: []Edge{},
		},
		"pod within namespace adds one edge": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["namespace"]),
				testutil.Unstructured(t, resources["pod"]),
			},
			expected: []Edge{
				{
					From: testutil.ToIdentifier(t, resources["pod"]),
					To:   testutil.ToIdentifier(t, resources["namespace"]),
				},
			},
		},
		"pod not in namespace does not add edge": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["namespace"]),
				testutil.Unstructured(t, resources["default-pod"]),
			},
			expected: []Edge{},
		},
		"pod, secret, and namespace adds two edges": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["namespace"]),
				testutil.Unstructured(t, resources["secret"]),
				testutil.Unstructured(t, resources["pod"]),
			},
			expected: []Edge{
				{
					From: testutil.ToIdentifier(t, resources["pod"]),
					To:   testutil.ToIdentifier(t, resources["namespace"]),
				},
				{
					From: testutil.ToIdentifier(t, resources["secret"]),
					To:   testutil.ToIdentifier(t, resources["namespace"]),
				},
			},
		},
		"one pod in namespace, one not, adds only one edge": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["namespace"]),
				testutil.Unstructured(t, resources["default-pod"]),
				testutil.Unstructured(t, resources["pod"]),
			},
			expected: []Edge{
				{
					From: testutil.ToIdentifier(t, resources["pod"]),
					To:   testutil.ToIdentifier(t, resources["namespace"]),
				},
			},
		},
	}

	for tn, tc := range testCases {
		t.Run(tn, func(t *testing.T) {
			g := New()
			addNamespaceEdges(g, tc.objs)
			actual := g.GetEdges()
			verifyEdges(t, tc.expected, actual)
		})
	}
}

func TestAddCRDEdges(t *testing.T) {
	testCases := map[string]struct {
		objs     []*unstructured.Unstructured
		expected []Edge
	}{
		"no CRD objects adds no graph edges": {
			objs:     []*unstructured.Unstructured{},
			expected: []Edge{},
		},
		"single namespace adds no graph edges": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["crd"]),
			},
			expected: []Edge{},
		},
		"two custom resources adds no graph edges": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["crontab1"]),
				testutil.Unstructured(t, resources["crontab2"]),
			},
			expected: []Edge{},
		},
		"two custom resources with crd adds two edges": {
			objs: []*unstructured.Unstructured{
				testutil.Unstructured(t, resources["crd"]),
				testutil.Unstructured(t, resources["crontab1"]),
				testutil.Unstructured(t, resources["crontab2"]),
			},
			expected: []Edge{
				{
					From: testutil.ToIdentifier(t, resources["crontab1"]),
					To:   testutil.ToIdentifier(t, resources["crd"]),
				},
				{
					From: testutil.ToIdentifier(t, resources["crontab2"]),
					To:   testutil.ToIdentifier(t, resources["crd"]),
				},
			},
		},
	}

	for tn, tc := range testCases {
		t.Run(tn, func(t *testing.T) {
			g := New()
			addCRDEdges(g, tc.objs)
			actual := g.GetEdges()
			verifyEdges(t, tc.expected, actual)
		})
	}
}

// verifyEdges ensures the slices of directed Edges contain the same elements.
// Order is not important.
func verifyEdges(t *testing.T, expected []Edge, actual []Edge) {
	if len(expected) != len(actual) {
		t.Fatalf("expected (%d) edges, got (%d)", len(expected), len(actual))
		return
	}
	for _, actualEdge := range actual {
		if !containsEdge(expected, actualEdge) {
			t.Errorf("actual Edge (%v) not found in expected Edges", actualEdge)
			return
		}
	}
}

// containsEdge return true if the passed Edge is in the slice of Edges;
// false otherwise.
func containsEdge(edges []Edge, edge Edge) bool {
	for _, e := range edges {
		if e.To == edge.To && e.From == edge.From {
			return true
		}
	}
	return false
}
