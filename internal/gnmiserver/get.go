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
	"encoding/json"
	"fmt"
	"time"

	"github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/yndd/ndd-yang/pkg/yparser"

	"github.com/yndd/nddc-metric-collector/internal/controllers/collector"
)

func (s *Server) Get(ctx context.Context, req *gnmi.GetRequest) (*gnmi.GetResponse, error) {
	ok := s.unaryRPCsem.TryAcquire(1)
	if !ok {
		return nil, status.Errorf(codes.ResourceExhausted, "max number of Unary RPC reached")
	}
	defer s.unaryRPCsem.Release(1)

	log := s.log.WithValues("Type", req.GetType())
	if req.GetPath() != nil {
		log.Debug("Get...", "Path", yparser.GnmiPath2XPath(req.GetPath()[0], true))
	} else {
		log.Debug("Get...")
	}

	// we overwrite the gnmi prefix for now
	prefix := &gnmi.Path{Target: collector.GnmiTarget, Origin: collector.GnmiOrigin}

	//log.Debug("GNMI GET...")
	updates, err := s.HandleGet(yparser.GetDataType(req.GetType()), prefix, req.GetPath())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, fmt.Sprintf("Error: %s", err))
	}

	return &gnmi.GetResponse{
		Notification: []*gnmi.Notification{
			{
				Timestamp: time.Now().UnixNano(),
				Prefix:    req.GetPrefix(),
				Update:    updates,
			},
		},
	}, nil
}

func (s *Server) HandleGet(cacheType string, prefix *gnmi.Path, reqPaths []*gnmi.Path) ([]*gnmi.Update, error) {
	//var err error
	updates := make([]*gnmi.Update, 0)

	cache := s.GetConfigCache()
	target := collector.GnmiTarget
	if cacheType == yparser.CacheTypeState {
		cache = s.GetStateCache()
	}
	if cacheType == yparser.CacheTypeTarget {
		// TO BE REMOVED
		cache = s.GetTargetCache()
		target = "leaf1"
		prefix = &gnmi.Path{Target: target}
	}

	for _, path := range reqPaths {
		x, err := cache.GetJson(target, prefix, path)
		if err != nil {
			return nil, err
		}

		if updates, err = appendUpdateResponse(x, path, updates); err != nil {
			return nil, err
		}
	}
	return updates, nil
}

func appendUpdateResponse(data interface{}, path *gnmi.Path, updates []*gnmi.Update) ([]*gnmi.Update, error) {
	var err error
	var d []byte
	if data != nil {
		d, err = json.Marshal(data)
		if err != nil {
			return nil, err
		}
	}

	upd := &gnmi.Update{
		Path: path,
		Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_JsonVal{JsonVal: d}},
	}
	updates = append(updates, upd)
	return updates, nil
}
