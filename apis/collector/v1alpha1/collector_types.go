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
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/yndd/ndd-yang/pkg/yparser"
)

func (r *NddccollectorCollector) GetPaths() []*gnmi.Path {
	paths := []*gnmi.Path{}
	for _, m := range r.Metric {
		for _, p := range m.Paths {
			paths = append(paths, yparser.Xpath2GnmiPath(*p, 0))
		}
	}
	return paths
}

func (r *NddccollectorCollector) GetMetrics() map[string]*NddccollectorCollectorMetric {
	metrics := make(map[string]*NddccollectorCollectorMetric, 0)
	for _, m := range r.Metric {
		if *m.AdminState == "enable" {
			metrics[*m.Name] = m.DeepCopy()
		}
	}
	return metrics
}
