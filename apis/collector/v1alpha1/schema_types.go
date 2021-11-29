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

// Nddccollector struct
type Nddccollector struct {
	Collector *NddccollectorCollector `json:"collector,omitempty"`
}

// NddccollectorCollector struct
type NddccollectorCollector struct {
	Metric []*NddccollectorCollectorMetric `json:"metric,omitempty"`
}

// NddccollectorCollectorMetric struct
type NddccollectorCollectorMetric struct {
	AdminState  *string `json:"admin-state,omitempty"`
	Prefix      *string `json:"prefix,omitempty"`
	Description *string `json:"description,omitempty"`
	LastUpdate  *string `json:"last-update,omitempty"`
	Name        *string `json:"name"`
	//+kubebuilder:validation:MinItems=0
	//+kubebuilder:validation:MaxItems=16
	Paths []*string `json:"paths,omitempty"`
}

// Root is the root of the schema
type Root struct {
	CollectorNddccollector *Nddccollector `json:"nddc-metric-collector,omitempty"`
}
