//go:build !ignore_autogenerated
// +build !ignore_autogenerated

/*
Copyright 2021 NDD.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
// Code generated by ndd-gen. DO NOT EDIT.

package v1alpha1

import nddv1 "github.com/yndd/ndd-runtime/apis/common/v1"

// GetActive of this CollectorCollector.
func (mg *CollectorCollector) GetActive() bool {
	return mg.Spec.Active
}

// GetCondition of this CollectorCollector.
func (mg *CollectorCollector) GetCondition(ck nddv1.ConditionKind) nddv1.Condition {
	return mg.Status.GetCondition(ck)
}

// GetDeletionPolicy of this CollectorCollector.
func (mg *CollectorCollector) GetDeletionPolicy() nddv1.DeletionPolicy {
	return mg.Spec.DeletionPolicy
}

// GetExternalLeafRefs of this CollectorCollector.
func (mg *CollectorCollector) GetExternalLeafRefs() []string {
	return mg.Status.ExternalLeafRefs
}

// GetNetworkNodeReference of this CollectorCollector.
func (mg *CollectorCollector) GetNetworkNodeReference() *nddv1.Reference {
	return mg.Spec.NetworkNodeReference
}

// GetResourceIndexes of this CollectorCollector.
func (mg *CollectorCollector) GetResourceIndexes() map[string]string {
	return mg.Status.ResourceIndexes
}

// GetTarget of this CollectorCollector.
func (mg *CollectorCollector) GetTarget() []string {
	return mg.Status.Target
}

// SetActive of this CollectorCollector.
func (mg *CollectorCollector) SetActive(b bool) {
	mg.Spec.Active = b
}

// SetConditions of this CollectorCollector.
func (mg *CollectorCollector) SetConditions(c ...nddv1.Condition) {
	mg.Status.SetConditions(c...)
}

// SetDeletionPolicy of this CollectorCollector.
func (mg *CollectorCollector) SetDeletionPolicy(r nddv1.DeletionPolicy) {
	mg.Spec.DeletionPolicy = r
}

// SetExternalLeafRefs of this CollectorCollector.
func (mg *CollectorCollector) SetExternalLeafRefs(n []string) {
	mg.Status.ExternalLeafRefs = n
}

// SetNetworkNodeReference of this CollectorCollector.
func (mg *CollectorCollector) SetNetworkNodeReference(r *nddv1.Reference) {
	mg.Spec.NetworkNodeReference = r
}

// SetResourceIndexes of this CollectorCollector.
func (mg *CollectorCollector) SetResourceIndexes(n map[string]string) {
	mg.Status.ResourceIndexes = n
}

// SetTarget of this CollectorCollector.
func (mg *CollectorCollector) SetTarget(t []string) {
	mg.Status.Target = t
}
