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

package gnmiserver

import (
	"context"
	"fmt"
	"time"

	"github.com/openconfig/gnmi/path"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/yndd/ndd-yang/pkg/dispatcher"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/yndd/nddc-metric-collector/internal/controllers/collector"
)

func (s *Server) Set(ctx context.Context, req *gnmi.SetRequest) (*gnmi.SetResponse, error) {

	ok := s.unaryRPCsem.TryAcquire(1)
	if !ok {
		return nil, status.Errorf(codes.ResourceExhausted, errMaxNbrOfUnaryRPCReached)
	}
	defer s.unaryRPCsem.Release(1)

	numUpdates := len(req.GetUpdate())
	numReplaces := len(req.GetReplace())
	numDeletes := len(req.GetDelete())
	if numUpdates+numReplaces+numDeletes == 0 {
		return nil, status.Errorf(codes.InvalidArgument, errMissingPathsInGNMISet)
	}

	log := s.log.WithValues("numUpdates", numUpdates, "numReplaces", numReplaces, "numDeletes", numDeletes)
	prefix := req.GetPrefix()
	log.Debug("Set", "prefix", prefix)

	updateObjects := make(map[string]*gnmi.Path)
	deleteObjects := make(map[string]*gnmi.Path)
	if numReplaces > 0 {
		for _, u := range req.GetReplace() {
			if err := s.UpdateConfigCache(prefix, u); err != nil {
				return nil, status.Errorf(codes.InvalidArgument, fmt.Sprintf("Error: %v", err))
			}
			// handles config updates as a transaction aligned with the k8s crd controller
			// aggregate all updates and map them to the resources in the application logic
			//log.Debug("Replace Path", "Path", u.GetPath())
			if pe := s.dispatcher.GetPathElem(u.GetPath()); pe != nil {
				key, path := getPath2Process(u.GetPath(), pe)
				//log.Debug("Replace Path", "Key", key, "path", path)
				if _, ok := updateObjects[key]; !ok {
					updateObjects[key] = path
				}
			}
		}
	}

	if numUpdates > 0 {
		for _, u := range req.GetUpdate() {
			if err := s.UpdateConfigCache(prefix, u); err != nil {
				return nil, status.Errorf(codes.InvalidArgument, fmt.Sprintf("Error: %v", err))
			}
			// handles config updates as a transaction aligned with the k8s crd controller
			// aggregate all updates and map them to the resources in the application logic
			if pe := s.dispatcher.GetPathElem(u.GetPath()); pe != nil {
				key, path := getPath2Process(u.GetPath(), pe)
				if _, ok := updateObjects[key]; !ok {
					updateObjects[key] = path
				}
			}
		}
	}

	if numDeletes > 0 {
		for _, p := range req.GetDelete() {
			if err := s.DeleteConfigCache(prefix, p); err != nil {
				return nil, status.Errorf(codes.InvalidArgument, fmt.Sprintf("Error: %v", err))
			}
			// handles config updates as a transaction aligned with the k8s crd controller
			// aggregate all updates and map them to the resources in the application logic
			if pe := s.dispatcher.GetPathElem(p); pe != nil {
				if len(p.GetElem()) != len(pe) {
					//update of an existing element
					key, path := getPath2Process(p, pe)
					if _, ok := updateObjects[key]; !ok {
						updateObjects[key] = path
					}

				} else {
					//delete
					key, path := getPath2Process(p, pe)
					if _, ok := deleteObjects[key]; !ok {
						deleteObjects[key] = path
					}
				}

			}
		}

		// Delete the resources in the application logic as a single transaction
		for _, path := range deleteObjects {
			if _, err := s.rootResource.HandleConfigEvent(dispatcher.OperationDelete, prefix, path.GetElem(), nil); err != nil {
				return nil, status.Errorf(codes.InvalidArgument, fmt.Sprintf("Error: %v", err))
			}
		}
	}

	// Update the resources in the application logic as a single transaction
	log.Debug("Set", "updateObjects", updateObjects)
	for _, path := range updateObjects {
		// get the data from the cache as a big json blob
		d, err := s.GetConfigCache().GetJson(collector.GnmiTarget, prefix, path)
		if err != nil {
			return nil, err
		}

		if _, err := s.rootResource.HandleConfigEvent(dispatcher.OperationUpdate, prefix, path.GetElem(), d); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, fmt.Sprintf("Error: %v", err))
		}
	}

	cfg, _ := s.GetConfig()
	state, _ := s.GetState()
	log.Debug("Set Result Final Config Data", "schema", cfg)
	log.Debug("Set Result Final State  Data", "schema", state)

	return &gnmi.SetResponse{
		Response: []*gnmi.UpdateResult{
			{
				Timestamp: time.Now().UnixNano(),
			},
		}}, nil
}

func (s *Server) UpdateConfigCache(prefix *gnmi.Path, u *gnmi.Update) error {
	//log.Debug("Replace", "Update", u)
	n, err := s.GetConfigCache().GetNotificationFromUpdate(prefix, u)
	if err != nil {
		//log.Debug("GetNotificationFromUpdate Error", "Notification", n, "Error", err)
		return err
	}
	//log.Debug("Replace", "Notification", n)
	if n != nil {
		if err := s.GetConfigCache().GnmiUpdate(collector.GnmiTarget, n); err != nil {
			//log.Debug("GnmiUpdate Error", "Notification", n, "Error", err)
			return err
		}
	}
	return nil
}

func (s *Server) DeleteConfigCache(prefix *gnmi.Path, p *gnmi.Path) error {
	// delete from config cache
	n, err := s.GetConfigCache().GetNotificationFromDelete(prefix, p)
	if err != nil {
		return err
	}
	if err := s.GetConfigCache().GnmiUpdate(collector.GnmiTarget, n); err != nil {
		return err
	}
	return nil
}

// getPath2Process resolves the keys in the pathElem
// returns the resolved path based on the pathElem returned from lpm cache lookup
// returns the key which is using path.Strings where each element in the path.Strings
// is delineated by a .
func getPath2Process(p *gnmi.Path, pe []*gnmi.PathElem) (string, *gnmi.Path) {
	newPathElem := make([]*gnmi.PathElem, 0)
	var key string
	for i, elem := range pe {
		e := &gnmi.PathElem{
			Name: elem.GetName(),
		}
		if len(p.GetElem()[i].GetKey()) != 0 {
			e.Key = make(map[string]string)
			for keyName, keyValue := range p.GetElem()[i].GetKey() {
				e.Key[keyName] = keyValue
			}
		}
		newPathElem = append(newPathElem, e)
	}
	newPath := &gnmi.Path{Elem: newPathElem}
	stringlist := path.ToStrings(p, false)[:len(path.ToStrings(newPath, false))]
	for _, s := range stringlist {
		key = s + "."
	}
	return key, newPath
}
