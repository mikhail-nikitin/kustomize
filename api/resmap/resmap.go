// Copyright 2019 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

// Package resmap implements a map from ResId to Resource that
// tracks all resources in a kustomization.
package resmap

import (
	"sigs.k8s.io/kustomize/api/ifc"
	"sigs.k8s.io/kustomize/api/resid"
	"sigs.k8s.io/kustomize/api/resource"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// A Transformer modifies an instance of ResMap.
type Transformer interface {
	// Transform modifies data in the argument,
	// e.g. adding labels to resources that can be labelled.
	Transform(m ResMap) error
}

// A Generator creates an instance of ResMap.
type Generator interface {
	Generate() (ResMap, error)
}

// Something that's configurable accepts an
// instance of PluginHelpers and a raw config
// object (YAML in []byte form).
type Configurable interface {
	Config(h *PluginHelpers, config []byte) error
}

// NewPluginHelpers makes an instance of PluginHelpers.
func NewPluginHelpers(ldr ifc.Loader, v ifc.Validator, rf *Factory) *PluginHelpers {
	return &PluginHelpers{ldr: ldr, v: v, rf: rf}
}

// PluginHelpers holds things that any or all plugins might need.
// This should be available to each plugin, in addition to
// any plugin-specific configuration.
type PluginHelpers struct {
	ldr ifc.Loader
	v   ifc.Validator
	rf  *Factory
}

func (c *PluginHelpers) Loader() ifc.Loader {
	return c.ldr
}

func (c *PluginHelpers) ResmapFactory() *Factory {
	return c.rf
}

func (c *PluginHelpers) Validator() ifc.Validator {
	return c.v
}

type GeneratorPlugin interface {
	Generator
	Configurable
}

type TransformerPlugin interface {
	Transformer
	Configurable
}

// ResMap is an interface describing operations on the
// core kustomize data structure, a list of Resources.
//
// Every Resource has two ResIds: OrgId and CurId.
//
// In a ResMap, no two resources may have the same CurId,
// but they may have the same OrgId.  The latter can happen
// when mixing two or more different overlays apply different
// transformations to a common base.  When looking for a
// resource to transform, try the OrgId first, and if this
// fails or finds too many, it might make sense to then try
// the CurrId.  Depends on the situation.
type ResMap interface {
	// Size reports the number of resources.
	Size() int

	// Resources provides a discardable slice
	// of resource pointers, returned in the order
	// as appended.
	Resources() []*resource.Resource

	// Append adds a Resource. Error on CurId collision.
	//
	// A class invariant of ResMap is that all of its
	// resources must differ in their value of
	// CurId(), aka current Id.  The Id is the tuple
	// of {namespace, group, version, kind, name}
	// (see ResId).
	//
	// This invariant reflects the invariant of a
	// kubernetes cluster, where if one tries to add
	// a resource to the cluster whose Id matches
	// that of a resource already in the cluster,
	// only two outcomes are allowed.  Either the
	// incoming resource is _merged_ into the existing
	// one, or the incoming resource is rejected.
	// One cannot end up with two resources
	// in the cluster with the same Id.
	Append(*resource.Resource) error

	// AppendAll appends another ResMap to self,
	// failing on any CurId collision.
	AppendAll(ResMap) error

	// AbsorbAll appends, replaces or merges the contents
	// of another ResMap into self,
	// allowing and sometimes demanding ID collisions.
	// A collision would be demanded, say, when a generated
	// ConfigMap has the "replace" option in its generation
	// instructions, meaning it _must_ replace
	// something in the known set of resources.
	// If a resource id for resource X is found to already
	// be in self, then the behavior field for X must
	// be BehaviorMerge or BehaviorReplace. If X is not in
	// self, then its behavior _cannot_ be merge or replace.
	AbsorbAll(ResMap) error

	// AsYaml returns the yaml form of resources.
	AsYaml() ([]byte, error)

	// GetByIndex returns a resource at the given index,
	// nil if out of range.
	GetByIndex(int) *resource.Resource

	// GetIndexOfCurrentId returns the index of the resource
	// with the given CurId.
	// Returns error if there is more than one match.
	// Returns (-1, nil) if there is no match.
	GetIndexOfCurrentId(id resid.ResId) (int, error)

	// GetMatchingResourcesByCurrentId returns the resources
	// who's CurId is matched by the argument.
	GetMatchingResourcesByCurrentId(matches IdMatcher) []*resource.Resource

	// GetMatchingResourcesByOriginalId returns the resources
	// who's OriginalId is matched by the argument.
	GetMatchingResourcesByOriginalId(matches IdMatcher) []*resource.Resource

	// GetByCurrentId is shorthand for calling
	// GetMatchingResourcesByCurrentId with a matcher requiring
	// an exact match, returning an error on multiple or no matches.
	GetByCurrentId(resid.ResId) (*resource.Resource, error)

	// GetByOriginalId is shorthand for calling
	// GetMatchingResourcesByOriginalId with a matcher requiring
	// an exact match, returning an error on multiple or no matches.
	GetByOriginalId(resid.ResId) (*resource.Resource, error)

	// GetById is a helper function which first
	// attempts GetByOriginalId, then GetByCurrentId,
	// returning an error if both fail to find a single
	// match.
	GetById(resid.ResId) (*resource.Resource, error)

	// GroupedByCurrentNamespace returns a map of namespace
	// to a slice of *Resource in that namespace.
	// Resources for whom IsNamespaceableKind is false are
	// are not included at all (see NonNamespaceable).
	// Resources with an empty namespace are placed
	// in the resid.DefaultNamespace entry.
	GroupedByCurrentNamespace() map[string][]*resource.Resource

	// GroupByOrginalNamespace performs as GroupByNamespace
	// but use the original namespace instead of the current
	// one to perform the grouping.
	GroupedByOriginalNamespace() map[string][]*resource.Resource

	// NonNamespaceable returns a slice of resources that
	// cannot be placed in a namespace, e.g.
	// Node, ClusterRole, Namespace itself, etc.
	NonNamespaceable() []*resource.Resource

	// AllIds returns all CurrentIds.
	AllIds() []resid.ResId

	// Replace replaces the resource with the matching CurId.
	// Error if there's no match or more than one match.
	// Returns the index where the replacement happened.
	Replace(*resource.Resource) (int, error)

	// Remove removes the resource whose CurId matches the argument.
	// Error if not found.
	Remove(resid.ResId) error

	// Clear removes all resources and Ids.
	Clear()

	// SubsetThatCouldBeReferencedByResource returns a ResMap subset
	// of self with resources that could be referenced by the
	// resource argument.
	// This is a filter; it excludes things that cannot be
	// referenced by the resource, e.g. objects in other
	// namespaces. Cluster wide objects are never excluded.
	SubsetThatCouldBeReferencedByResource(*resource.Resource) ResMap

	// DeepCopy copies the ResMap and underlying resources.
	DeepCopy() ResMap

	// ShallowCopy copies the ResMap but
	// not the underlying resources.
	ShallowCopy() ResMap

	// ErrorIfNotEqualSets returns an error if the
	// argument doesn't have the same resources as self.
	// Ordering is _not_ taken into account,
	// as this function was solely used in tests written
	// before internal resource order was maintained,
	// and those tests are initialized with maps which
	// by definition have random ordering, and will
	// fail spuriously.
	// TODO: modify tests to not use resmap.FromMap,
	// TODO: - and replace this with a stricter equals.
	ErrorIfNotEqualSets(ResMap) error

	// ErrorIfNotEqualLists returns an error if the
	// argument doesn't have the resource objects
	// data as self, in the same order.
	// Meta information is ignored; this is similar
	// to comparing the AsYaml() strings, but allows
	// for more informed errors on not equals.
	ErrorIfNotEqualLists(ResMap) error

	// Debug prints the ResMap.
	Debug(title string)

	// Select returns a list of resources that
	// are selected by a Selector
	Select(types.Selector) ([]*resource.Resource, error)

	// ToRNodeSlice converts the resources in the resmp
	// to a list of RNodes
	ToRNodeSlice() ([]*yaml.RNode, error)

	// ApplySmPatch applies a strategic-merge patch to the
	// selected set of resources.
	ApplySmPatch(
		selectedSet *resource.IdSet, patch *resource.Resource) error

	// Remove annotations used exclusively by the kustomize build process.
	RemoveIdAnnotations()
}
