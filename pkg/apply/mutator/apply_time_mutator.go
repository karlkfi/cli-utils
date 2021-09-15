// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package mutator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"sigs.k8s.io/cli-utils/pkg/apply/cache"
	"sigs.k8s.io/cli-utils/pkg/kyq"
	"sigs.k8s.io/cli-utils/pkg/object/mutation"
	"sigs.k8s.io/yaml"
)

// ApplyTimeMutator mutates a resource by injecting values specified by the
// apply-time-mutation annotation.
// The optional ResourceCache will be used to speed up source resource lookups,
// if specified.
// Implements the Mutator interface
type ApplyTimeMutator struct {
	Client        dynamic.Interface
	Mapper        meta.RESTMapper
	ResourceCache cache.ResourceCache
}

// Name returns a mutator identifier for logging.
func (atm *ApplyTimeMutator) Name() string {
	return "ApplyTimeMutator"
}

// Mutate parses the apply-time-mutation annotation and loops through the
// substitutions, applying each of them to the supplied target object.
// Returns true with a reason, if mutation was performed.
func (atm *ApplyTimeMutator) Mutate(ctx context.Context, obj *unstructured.Unstructured) (bool, string, error) {
	mutated := false
	reason := ""

	targetRef := mutation.NewResourceReference(obj)

	if !mutation.HasAnnotation(obj) {
		return mutated, reason, nil
	}

	subs, err := mutation.ReadAnnotation(obj)
	if err != nil {
		return mutated, reason, fmt.Errorf("failed to read jsonpath field in target resource (%v): %w", targetRef, err)
	}

	klog.V(4).Infof("target resource (%v):\n%s", targetRef, yamlStringer{obj})

	// validate no self-references
	for _, sub := range subs {
		if targetRef.Equal(sub.SourceRef) {
			return mutated, reason, fmt.Errorf("invalid self-reference (%v)", sub.SourceRef)
		}
	}

	for _, sub := range subs {
		sourceRef := sub.SourceRef

		// lookup source resource from cache or cluster
		sourceObj, err := atm.getObject(ctx, sourceRef)
		if err != nil {
			return mutated, reason, fmt.Errorf("failed to get source resource (%v): %w", sourceRef, err)
		}

		klog.V(4).Infof("source resource (%v):\n%s", targetRef, yamlStringer{sourceObj})

		// lookup target field in target resource
		targetValue, _, err := readFieldValue(obj, sub.TargetPath)
		if err != nil {
			return mutated, reason, fmt.Errorf("failed to read field %q from target resource (%v): %w", sub.TargetPath, targetRef, err)
		}

		// lookup source field in source resource
		sourceValue, found, err := readFieldValue(sourceObj, sub.SourcePath)
		if err != nil {
			return mutated, reason, fmt.Errorf("failed to read field %q from source resource (%v): %w", sub.SourcePath, sourceRef, err)
		}
		if !found {
			return mutated, reason, fmt.Errorf("source field %q not present in source resource (%v)", sub.SourcePath, sourceRef)
		}

		var newValue interface{}
		if sub.Token == "" {
			// token not specified, replace the entire target value with the source value
			newValue = sourceValue
		} else {
			// token specified, substitute token for source field value in target field value
			targetValueString, ok := targetValue.(string)
			if !ok {
				return mutated, reason, fmt.Errorf("token is specified, but target field value is %T, expected string", targetValue)
			}

			sourceValueString, err := valueToString(sourceValue)
			if err != nil {
				return mutated, reason, fmt.Errorf("failed to stringify source field value (%v): %w", targetRef, err)
			}

			// Substitute token for source field value, if present.
			// If not present, do nothing. This is common on updates.
			newValue = strings.ReplaceAll(targetValueString, sub.Token, sourceValueString)
		}

		klog.V(5).Infof("substitution on (%v): source=%q, token=%q, old=%q, new=%q",
			targetRef, sourceValue, sub.Token, targetValue, newValue)

		// update target field in target resource
		err = writeFieldValue(obj, sub.TargetPath, newValue)
		if err != nil {
			return mutated, reason, fmt.Errorf("failed to set field in target resource (%v): %w", targetRef, err)
		}

		mutated = true
		reason = fmt.Sprintf("resource contained annotation: %s", mutation.Annotation)
	}

	if mutated {
		klog.V(4).Infof("mutated target resource (%v):\n%s", targetRef, yamlStringer{obj})
	}

	return mutated, reason, nil
}

// getObject returns a cached resource, if cached and cache exists, otherwise
// the resource is retrieved from the cluster.
func (atm *ApplyTimeMutator) getObject(ctx context.Context, ref mutation.ResourceReference) (*unstructured.Unstructured, error) {
	// validate resource reference
	id, err := mutation.ResourceReferenceToObjMeta(ref)
	if err != nil {
		return nil, fmt.Errorf("failed to validate resource reference (%v): %w", ref, err)
	}

	// get resource from cache
	if atm.ResourceCache != nil {
		obj, found := atm.ResourceCache.Get(id)
		if found && obj != nil {
			return obj, nil
		}
	}

	// lookup resource using group api version, if specified
	sourceGvk := ref.GroupVersionKind()
	var mapping *meta.RESTMapping
	if sourceGvk.Version != "" {
		mapping, err = atm.Mapper.RESTMapping(sourceGvk.GroupKind(), sourceGvk.Version)
	} else {
		mapping, err = atm.Mapper.RESTMapping(sourceGvk.GroupKind())
	}
	if err != nil {
		return nil, fmt.Errorf("failed to identify resource mapping: %w", err)
	}

	// lookup resource from cluster
	namespacedClient := atm.Client.Resource(mapping.Resource).Namespace(ref.Namespace)
	obj, err := namespacedClient.Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		// TODO: different error message for 404 not found?
		return nil, fmt.Errorf("failed to retrieve resource from cluster: %w", err)
	}

	// add resource to cache
	if atm.ResourceCache != nil {
		atm.ResourceCache.Set(id, obj)
	}

	return obj, nil
}

func readFieldValue(obj *unstructured.Unstructured, path string) (interface{}, bool, error) {
	if path == "" {
		return nil, false, errors.New("empty path expression")
	}

	if strings.HasPrefix(path, "$.") {
		return nil, false, errors.New("path expression starts with '$.': yq expressions should start with '.'")
	}

	value, found, err := kyq.Get(obj.Object, path)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read resource field with path expression %q: %w", path, err)
	}
	return value, found, nil
}

func writeFieldValue(obj *unstructured.Unstructured, path string, value interface{}) error {
	if path == "" {
		return errors.New("empty path expression")
	}

	if strings.HasPrefix(path, "$.") {
		return errors.New("path expression starts with '$.': yq expressions should start with '.'")
	}

	err := kyq.Set(obj.Object, path, value)
	if err != nil {
		return fmt.Errorf("failed to read resource field with path expression %q: %w", path, err)
	}
	return nil
}

// valueToString converts an interface{} to a string, formatting as json for
// maps, lists. Designed to handle yaml/json/krm primitives.
func valueToString(value interface{}) (string, error) {
	var valueString string
	switch valueTyped := value.(type) {
	case string:
		valueString = valueTyped
	case int, int32, int64, float32, float64, bool:
		valueString = fmt.Sprintf("%v", valueTyped)
	default:
		jsonBytes, err := json.Marshal(valueTyped)
		if err != nil {
			return "", fmt.Errorf("failed to marshal value to json: %#v", value)
		}
		valueString = string(jsonBytes)
	}
	return valueString, nil
}

// yamlStringer delays YAML marshalling for logging until String() is called.
type yamlStringer struct {
	obj *unstructured.Unstructured
}

// String marshals the wrapped object to a YAML string. If serializing errors,
// the error string will be returned instead. This is primarily for use with
// verbose multi-line logging.
func (ys yamlStringer) String() string {
	yamlBytes, err := yaml.Marshal(ys.obj.Object)
	if err != nil {
		return fmt.Sprintf("failed to serialize as yaml: %s", err)
	}
	return string(yamlBytes)
}
