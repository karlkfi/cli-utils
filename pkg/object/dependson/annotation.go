// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0
//

package dependson

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	"sigs.k8s.io/cli-utils/pkg/object/mutation"
)

const (
	Annotation = "config.kubernetes.io/depends-on"
)

// HasAnnotation returns true if the config.kubernetes.io/depends-on annotation
// is present, false if not.
func HasAnnotation(u *unstructured.Unstructured) bool {
	if u == nil {
		return false
	}
	_, found := u.GetAnnotations()[Annotation]
	return found
}

// ReadAnnotation reads the depends-on annotation and parses the the set of
// object references.
func ReadAnnotation(obj *unstructured.Unstructured) (DependencySet, error) {
	depSet := DependencySet{}
	if obj == nil {
		return depSet, nil
	}
	depSetStr, found := obj.GetAnnotations()[Annotation]
	if !found {
		return depSet, nil
	}
	objRef := mutation.NewResourceReference(obj)
	klog.V(5).Infof("parsing annotation %q on object %q", Annotation, objRef)

	depSet, err := ParseDependencySet(depSetStr)
	if err != nil {
		return depSet, fmt.Errorf("failed to parse annotation %q on object %q: %w",
			Annotation, objRef, err)
	}
	return depSet, nil
}

// WriteAnnotation updates the supplied unstructured object to add the
// depends-on annotation. The value is a string of objmetas delimited by commas.
// Each objmeta is formatted as "${group}/${kind}/${name}" if cluster-scoped or
// "${group}/namespaces/${namespace}/${kind}/${name}" if namespace-scoped.
func WriteAnnotation(obj *unstructured.Unstructured, depSet DependencySet) error {
	if obj == nil {
		return errors.New("object is nil")
	}
	if depSet.Equal(DependencySet{}) {
		return errors.New("dependency set is empty")
	}

	depSetStr, err := FormatDependencySet(depSet)
	if err != nil {
		objRef := mutation.NewResourceReference(obj)
		return fmt.Errorf("failed to format annotation %q on object %q: %w",
			Annotation, objRef, err)
	}

	a := obj.GetAnnotations()
	if a == nil {
		a = map[string]string{}
	}
	a[Annotation] = depSetStr
	obj.SetAnnotations(a)
	return nil
}
