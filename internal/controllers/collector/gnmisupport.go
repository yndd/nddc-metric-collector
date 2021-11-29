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

package collector

import (
	"encoding/json"
	"fmt"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/pkg/errors"
	"github.com/yndd/ndd-yang/pkg/yentry"
	"github.com/yndd/ndd-yang/pkg/yparser"
)

const (
	GnmiTarget = "dummy"
	GnmiOrigin = "collector"
)

// processObserve
// 0. marshal/unmarshal data
// 1. check if resource exists
// 2. remove parent hierarchical elements from spec
// TODO 3. remove resource hierarchicaal elements from gnmi response
// 4. transform the data in gnmi to process the delta
// 5. find the resource delta: updates and/or deletes in gnmi
func processObserve(rootPath *gnmi.Path, hierPaths []*gnmi.Path, specData interface{}, resp *gnmi.GetResponse, rootSchema *yentry.Entry) (bool, []*gnmi.Path, []*gnmi.Update, error) {
	// prepare the input data to compare against the response data
	x1, err := processSpecData(rootPath, specData)
	if err != nil {
		return false, nil, nil, err
	}

	// validate gnmi resp information
	var x2 interface{}
	if len(resp.GetNotification()) != 0 && len(resp.GetNotification()[0].GetUpdate()) != 0 {
		// get value from gnmi get response
		x2, err = yparser.GetValue(resp.GetNotification()[0].GetUpdate()[0].Val)
		if err != nil {
			return false, nil, nil, errors.Wrap(err, errJSONMarshal)
		}

		fmt.Printf("processObserve: raw x2: %v\n", x2)

		switch x2.(type) {
		case nil:
			// resource does not exist and we return
			// RESOURCE DOES NOT EXIST
			return false, nil, nil, nil

		}
		/*
			switch x := x2.(type) {
			case map[string]interface{}:
				if x[resourceName] == nil {
					// resource does not exist and we return
					// RESOURCE DOES NOT EXIST
					return false, nil, nil, nil
				}
			}
		*/
	}
	// RESOURCE EXISTS

	fmt.Printf("processObserve rootPath %s\n", yparser.GnmiPath2XPath(rootPath, true))
	// for the Spec data we remove the first element which is aligned with the last element of the rootPath
	// gnmi does not return this information hence to compare the spec data with the gnmi resp data we need to remove
	// the first element from the Spec
	switch x := x1.(type) {
	case map[string]interface{}:
		x1 = x[rootPath.GetElem()[len(rootPath.GetElem())-1].GetName()]
		fmt.Printf("processObserve x1 data: %v\n", x1)
	}
	// the gnmi response already comes without the last element in the return data
	fmt.Printf("processObserve x2 data: %v\n", x2)

	// remove hierarchical resource elements from the data to be able to compare the gnmi response
	// with the k8s Spec
	switch x := x2.(type) {
	case map[string]interface{}:
		for _, hierPath := range hierPaths {
			x2 = removeHierarchicalResourceData(x, hierPath)
		}
	}

	fmt.Printf("processObserve x2 data: %v\n", x2)
	// data is present
	// for lists with keys we need to create a list before calulating the paths since this is what
	// the object eventually happens to be based upon. We avoid having multiple entries in a list object
	// and hence we have to add this step
	//if len(rootPath.GetElem()[len(rootPath.GetElem())-1].GetKey()) != 0 {
	//	x1, err = yparser.AddDataToList(x1)
	//	if err != nil {
	//		return false, nil, nil, errors.Wrap(err, errWrongInputdata)
	//	}
	//}
	updatesx1, err := yparser.GetUpdatesFromJSON(rootPath, x1, rootSchema)
	if err != nil {
		return false, nil, nil, errors.Wrap(err, errJSONMarshal)
	}
	/*
		for _, update := range updatesx1 {
			log.Debug("Observe Fine Grane Updates X1", "Path", yparser.GnmiPath2XPath(update.Path, true), "Value", update.GetVal())
		}
	*/
	for _, update := range updatesx1 {
		fmt.Printf("processObserve x1 update, Path %s, Value %v\n", yparser.GnmiPath2XPath(update.Path, true), update.GetVal())
	}

	//updatesx1 := e.parser.GetUpdatesFromJSONDataGnmi(rootPath[0], e.parser.XpathToGnmiPath("/", 0), x1, resourceRefPathsIpamTenantNetworkinstanceIpprefix)
	//for _, update := range updatesx1 {
	//	log.Debug("Observe Fine Grane Updates X1", "Path", e.parser.GnmiPathToXPath(update.Path, true), "Value", update.GetVal())
	//}
	// for lists with keys we need to create a list before calulating the paths since this is what
	// the object eventually happens to be based upon. We avoid having multiple entries in a list object
	// and hence we have to add this step
	//if len(rootPath.GetElem()[len(rootPath.GetElem())-1].GetKey()) != 0 {
	//	x2, err = yparser.AddDataToList(x2)
	//	if err != nil {
	//		return false, nil, nil, errors.Wrap(err, errWrongInputdata)
	//	}
	//}
	updatesx2, err := yparser.GetUpdatesFromJSON(rootPath, x2, rootSchema)
	if err != nil {
		return false, nil, nil, errors.Wrap(err, errJSONMarshal)
	}
	for _, update := range updatesx2 {
		fmt.Printf("processObserve x2 update, Path %s, Value %v\n", yparser.GnmiPath2XPath(update.Path, true), update.GetVal())
	}
	//updatesx2 := e.parser.GetUpdatesFromJSONDataGnmi(rootPath[0], e.parser.XpathToGnmiPath("/", 0), x2, resourceRefPathsIpamTenantNetworkinstanceIpprefix)
	//for _, update := range updatesx2 {
	//	log.Debug("Observe Fine Grane Updates X2", "Path", e.parser.GnmiPathToXPath(update.Path, true), "Value", update.GetVal())
	//}
	deletes, updates, err := yparser.FindResourceDelta(updatesx1, updatesx2)
	return true, deletes, updates, err

}

// processCreate
// o. marshal/unmarshal data
// 1. transform the spec data to gnmi updates
func processCreate(rootPath *gnmi.Path, specData interface{}, rootSchema *yentry.Entry) ([]*gnmi.Update, error) {
	// prepare the input data to compare against the response data
	x1, err := processSpecData(rootPath, specData)
	if err != nil {
		return nil, err
	}

	// for lists with keys we need to create a list before calulating the paths since this is what
	// the object eventually happens to be based upon. We avoid having multiple entries in a list object
	// and hence we have to add this step
	/*
		if len(rootPath.GetElem()[len(rootPath.GetElem())-1].GetKey()) != 0 {
			x1, err = yparser.AddDataToList(x1)
			if err != nil {
				return nil, errors.Wrap(err, errWrongInputdata)
			}
		}
	*/
	fmt.Printf("processCreate rootPath %s\n", yparser.GnmiPath2XPath(rootPath, true))
	switch x := x1.(type) {
	case map[string]interface{}:
		x1 := x[rootPath.GetElem()[len(rootPath.GetElem())-1].GetName()]
		fmt.Printf("processCreate data %v\n", x1)
		return yparser.GetUpdatesFromJSON(rootPath, x1, rootSchema)
	}
	return nil, errors.New("wrong data structure")

}

//process Spec data marshals the data and remove the prent hierarchical keys
func processSpecData(rootPath *gnmi.Path, specData interface{}) (interface{}, error) {
	// prepare the input data to compare against the response data
	d, err := json.Marshal(specData)
	if err != nil {
		return nil, errors.Wrap(err, errJSONMarshal)
	}
	var x1 interface{}
	if err := json.Unmarshal(d, &x1); err != nil {
		return nil, errors.Wrap(err, errJSONUnMarshal)
	}
	// removes the parent hierarchical ids; they are there to define the parent in k8s so
	// we can define the full path in gnmi
	return yparser.RemoveHierIDsFomData(yparser.GetHierIDsFromPath(rootPath), x1), nil
}

func removeHierarchicalResourceData(x map[string]interface{}, hierPath *gnmi.Path) interface{} {
	// this is the last pathElem of the hierarchical path, which is to be deleted
	if len(hierPath.GetElem()) == 1 {
		delete(x, hierPath.GetElem()[0].GetName())
	} else {
		// there is more pathElem in the hierachical Path
		if xx, ok := x[hierPath.GetElem()[0].GetName()]; ok {
			switch x1 := xx.(type) {
			case map[string]interface{}:
				removeHierarchicalResourceData(x1, &gnmi.Path{Elem: hierPath.GetElem()[1:]})
			case []interface{}:
				for _, xxx := range x1 {
					switch x2 := xxx.(type) {
					case map[string]interface{}:
						removeHierarchicalResourceData(x2, &gnmi.Path{Elem: hierPath.GetElem()[1:]})
					}
				}
			default:
				// it can be that no data is present, so we ignore this
			}
		}
	}

	return x
}
