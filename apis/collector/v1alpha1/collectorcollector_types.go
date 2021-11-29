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

package v1alpha1

import (
	"reflect"

	nddv1 "github.com/yndd/ndd-runtime/apis/common/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// CollectorFinalizer is the name of the finalizer added to
	// Collector to block delete operations until the physical node can be
	// deprovisioned.
	CollectorFinalizer string = "collector.collector.nddc.yndd.io"
)

// Collector struct
type Collector struct {
	Metric []*CollectorMetric `json:"metric,omitempty"`
}

// CollectorMetric struct
type CollectorMetric struct {
	AdminState  *string `json:"admin-state,omitempty"`
	Prefix      *string `json:"prefix,omitempty"`
	Description *string `json:"description,omitempty"`
	Name        *string `json:"name"`
	//+kubebuilder:validation:MinItems=0
	//+kubebuilder:validation:MaxItems=16
	Paths []*string `json:"paths,omitempty"`
}

// CollectorParameters are the parameter fields of a Collector.
type CollectorParameters struct {
	CollectorCollector *Collector `json:"collector,omitempty"`
}

// CollectorObservation are the observable fields of a Collector.
type CollectorObservation struct {
	*Nddccollector `json:",inline"`
}

// A CollectorSpec defines the desired state of a Collector.
type CollectorSpec struct {
	nddv1.ResourceSpec `json:",inline"`
	ForNetworkNode     CollectorParameters `json:"forNetworkNode"`
}

// A CollectorStatus represents the observed state of a Collector.
type CollectorStatus struct {
	nddv1.ResourceStatus `json:",inline"`
	AtNetworkNode        CollectorObservation `json:"atNetworkNode,omitempty"`
}

// +kubebuilder:object:root=true

// CollectorCollector is the Schema for the Collector API
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="TARGET",type="string",JSONPath=".status.conditions[?(@.kind=='TargetFound')].status"
// +kubebuilder:printcolumn:name="STATUS",type="string",JSONPath=".status.conditions[?(@.kind=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNC",type="string",JSONPath=".status.conditions[?(@.kind=='Synced')].status"
// +kubebuilder:printcolumn:name="LOCALLEAFREF",type="string",JSONPath=".status.conditions[?(@.kind=='InternalLeafrefValidationSuccess')].status"
// +kubebuilder:printcolumn:name="EXTLEAFREF",type="string",JSONPath=".status.conditions[?(@.kind=='ExternalLeafrefValidationSuccess')].status"
// +kubebuilder:printcolumn:name="PARENTDEP",type="string",JSONPath=".status.conditions[?(@.kind=='ParentValidationSuccess')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={ndd,collector}
type CollectorCollector struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CollectorSpec   `json:"spec,omitempty"`
	Status CollectorStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CollectorCollectorList contains a list of Collectors
type CollectorCollectorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CollectorCollector `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CollectorCollector{}, &CollectorCollectorList{})
}

// Collector type metadata.
var (
	CollectorKindKind         = reflect.TypeOf(CollectorCollector{}).Name()
	CollectorGroupKind        = schema.GroupKind{Group: Group, Kind: CollectorKindKind}.String()
	CollectorKindAPIVersion   = CollectorKindKind + "." + GroupVersion.String()
	CollectorGroupVersionKind = GroupVersion.WithKind(CollectorKindKind)
)
