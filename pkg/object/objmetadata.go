// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0
//
// ObjMetadata is the minimal set of information to
// uniquely identify an object. The four fields are:
//
//   Group/Kind (NOTE: NOT version)
//   Namespace
//   Name
//
// We specifically do not use the "version", because
// the APIServer does not recognize a version as a
// different resource. This metadata is used to identify
// resources for pruning and teardown.

package object

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// Separates inventory fields. This string is allowable as a
	// ConfigMap key, but it is not allowed as a character in
	// resource name.
	fieldSeparator = "_"
	// Transform colons in the RBAC resource names to double
	// underscore.
	colonTranscoded = "__"
)

var (
	NilObjMetadata = ObjMetadata{}
)

// RBACGroupKind is a map of the RBAC resources. Needed since name validation
// is different than other k8s resources.
var RBACGroupKind = map[schema.GroupKind]bool{
	{Group: rbacv1.GroupName, Kind: "Role"}:               true,
	{Group: rbacv1.GroupName, Kind: "ClusterRole"}:        true,
	{Group: rbacv1.GroupName, Kind: "RoleBinding"}:        true,
	{Group: rbacv1.GroupName, Kind: "ClusterRoleBinding"}: true,
}

// ObjMetadata organizes and stores the indentifying information
// for an object. This struct (as a string) is stored in a
// inventory object to keep track of sets of applied objects.
type ObjMetadata struct {
	Namespace string
	Name      string
	GroupKind schema.GroupKind
}

// ObjMetas is a slice of ObjMetadata.
type ObjMetas []ObjMetadata

// Contains checks if the provided ObjMetadata exists in the ObjMetas slice.
func (oms ObjMetas) Contains(id ObjMetadata) bool {
	for _, om := range oms {
		if om == id {
			return true
		}
	}
	return false
}

// CreateObjMetadata returns an ObjMetadata struct filled
// with the passed values. This function normalizes and validates the
// passed fields and returns an error for bad parameters.
func CreateObjMetadata(namespace string, name string, gk schema.GroupKind) (ObjMetadata, error) {
	// Namespace can be empty, but name cannot.
	if name == "" {
		return NilObjMetadata, fmt.Errorf("empty name for object")
	}
	if gk.Kind == "" {
		return NilObjMetadata, fmt.Errorf("empty kind for object")
	}
	return ObjMetadata{
		Namespace: namespace,
		Name:      name,
		GroupKind: gk,
	}, nil
}

// ParseObjMetadata takes a string, splits it into its four fields,
// and returns an ObjMetadata struct storing the four fields.
// Example inventory string:
//
//   test-namespace_test-name_apps_ReplicaSet
//
// Returns an error if unable to parse and create the ObjMetadata
// struct.
//
// NOTE: name field can contain double underscore (__), which represents
// a colon. RBAC resources can have this additional character (:) in their name.
func ParseObjMetadata(s string) (ObjMetadata, error) {
	// Parse first field namespace
	index := strings.Index(s, fieldSeparator)
	if index == -1 {
		return NilObjMetadata, fmt.Errorf("unable to parse stored object metadata: %s", s)
	}
	namespace := s[:index]
	s = s[index+1:]
	// Next, parse last field kind
	index = strings.LastIndex(s, fieldSeparator)
	if index == -1 {
		return NilObjMetadata, fmt.Errorf("unable to parse stored object metadata: %s", s)
	}
	kind := s[index+1:]
	s = s[:index]
	// Next, parse next to last field group
	index = strings.LastIndex(s, fieldSeparator)
	if index == -1 {
		return NilObjMetadata, fmt.Errorf("unable to parse stored object metadata: %s", s)
	}
	group := s[index+1:]
	// Finally, second field name. Name may contain colon transcoded as double underscore.
	name := s[:index]
	name = strings.ReplaceAll(name, colonTranscoded, ":")
	// Check that there are no extra fields by search for fieldSeparator.
	if strings.Contains(name, fieldSeparator) {
		return NilObjMetadata, fmt.Errorf("too many fields within: %s", s)
	}
	// Create the ObjMetadata object from the four parsed fields.
	gk := schema.GroupKind{
		Group: group,
		Kind:  kind,
	}
	return CreateObjMetadata(namespace, name, gk)
}

// Equals compares two ObjMetadata and returns true if they are equal. This does
// not contain any special treatment for the extensions API group.
func (o *ObjMetadata) Equals(other *ObjMetadata) bool {
	if other == nil {
		return false
	}
	return *o == *other
}

// String create a string version of the ObjMetadata struct. For RBAC resources,
// the "name" field transcodes ":" into double underscore for valid storing
// as the label of a ConfigMap.
func (o *ObjMetadata) String() string {
	name := o.Name
	if _, exists := RBACGroupKind[o.GroupKind]; exists {
		name = strings.ReplaceAll(name, ":", colonTranscoded)
	}
	return fmt.Sprintf("%s%s%s%s%s%s%s",
		o.Namespace, fieldSeparator,
		name, fieldSeparator,
		o.GroupKind.Group, fieldSeparator,
		o.GroupKind.Kind)
}

// RuntimeToObjMeta extracts the object metadata information from a
// runtime.Object and returns it as ObjMetadata.
func RuntimeToObjMeta(obj runtime.Object) (ObjMetadata, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return NilObjMetadata, err
	}
	return CreateObjMetadata(accessor.GetNamespace(), accessor.GetName(),
		obj.GetObjectKind().GroupVersionKind().GroupKind())
}

// Hash returns a hash of the sorted strings from
// the object metadata, or an error if one occurred.
func Hash(objs []ObjMetadata) (string, error) {
	objStrs := make([]string, 0, len(objs))
	for _, obj := range objs {
		objStrs = append(objStrs, obj.String())
	}
	hashInt, err := calcHash(objStrs)
	if err != nil {
		return "", err
	}
	return strconv.FormatUint(uint64(hashInt), 16), nil
}

// calcHash returns an unsigned int32 representing the hash
// of the obj metadata strings. If there is an error writing bytes to
// the hash, then the error is returned; nil is returned otherwise.
// Used to quickly identify the set of resources in the inventory object.
func calcHash(objs []string) (uint32, error) {
	sort.Strings(objs)
	h := fnv.New32a()
	for _, obj := range objs {
		_, err := h.Write([]byte(obj))
		if err != nil {
			return uint32(0), err
		}
	}
	return h.Sum32(), nil
}

// SetDiff returns the slice of objects that exist in "a", but
// do not exist in "b" (A - B).
func SetDiff(setA []ObjMetadata, setB []ObjMetadata) []ObjMetadata {
	// Create a map of the elements of A
	m := make(map[ObjMetadata]struct{}, len(setA))
	for _, a := range setA {
		m[a] = struct{}{}
	}
	// Remove from A each element of B
	for _, b := range setB {
		delete(m, b) // OK to delete even if b not in m
	}
	// Create/return slice from the map of remaining items
	diff := make([]ObjMetadata, 0, len(m))
	for r := range m {
		diff = append(diff, r)
	}
	return diff
}

// Union returns the slice of objects that is the set of unique
// items of the merging of set A and set B.
func Union(setA []ObjMetadata, setB []ObjMetadata) []ObjMetadata {
	m := make(map[ObjMetadata]struct{}, len(setA)+len(setB))
	for _, a := range setA {
		m[a] = struct{}{}
	}
	for _, b := range setB {
		m[b] = struct{}{}
	}
	union := make([]ObjMetadata, 0, len(m))
	for u := range m {
		union = append(union, u)
	}
	return union
}

// SetEquals returns true if the slice of objects in setA equals
// the slice of objects in setB.
func SetEquals(setA []ObjMetadata, setB []ObjMetadata) bool {
	mapA := make(map[ObjMetadata]struct{}, len(setA))
	for _, a := range setA {
		mapA[a] = struct{}{}
	}
	mapB := make(map[ObjMetadata]struct{}, len(setB))
	for _, b := range setB {
		mapB[b] = struct{}{}
	}
	if len(mapA) != len(mapB) {
		return false
	}
	for b := range mapB {
		if _, exists := mapA[b]; !exists {
			return false
		}
	}
	return true
}
