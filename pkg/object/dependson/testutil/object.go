// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/cli-utils/pkg/object/dependson"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

// AddDependsOn returns a testutil.Mutator which adds the passed objects as a
// depends-on annotation to the object which is mutated. Multiple objects
// passed in means multiple depends on objects in the annotation separated
// by a comma.
func AddDependsOn(t *testing.T, deps ...object.ObjMetadata) testutil.Mutator {
	return dependsOnMutator{
		t:    t,
		deps: dependson.DependencySet(deps),
	}
}

// dependsOnMutator encapsulates fields for adding depends-on annotation
// to a test object. Implements the Mutator interface.
type dependsOnMutator struct {
	t    *testing.T
	deps dependson.DependencySet
}

// Mutate writes a depends-on annotation on the supplied object. The value of
// the annotation is a set of dependencies referencing the dependsOnMutator's
// depObjs.
func (d dependsOnMutator) Mutate(u *unstructured.Unstructured) {
	err := dependson.WriteAnnotation(u, d.deps)
	if !assert.NoError(d.t, err) {
		d.t.FailNow()
	}
}
