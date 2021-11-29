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
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/openconfig/gnmi/coalesce"
	"github.com/openconfig/gnmi/ctree"
	"github.com/openconfig/gnmi/match"
	"github.com/openconfig/gnmi/path"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmi/subscribe"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type syncMarker struct{}

type streamClient struct {
	target  string
	req     *gnmi.SubscribeRequest
	queue   *coalesce.Queue
	stream  gnmi.GNMI_SubscribeServer
	errChan chan<- error
}

func (s *Server) Subscribe(stream gnmi.GNMI_SubscribeServer) error {

	sc := &streamClient{
		stream: stream,
	}
	var err error
	sc.req, err = stream.Recv()
	switch {
	case err == io.EOF:
		return nil
	case err != nil:
		return err
	case sc.req.GetSubscribe() == nil:
		return status.Errorf(codes.InvalidArgument, errGetSubscribe)
	}
	sc.target = sc.req.GetSubscribe().GetPrefix().GetTarget()

	peer, _ := peer.FromContext(stream.Context())
	log := s.log.WithValues("mode", sc.req.GetSubscribe().GetMode(), "from", peer.Addr, "target", sc.target)
	log.Debug("Subscribe Request...")
	defer s.log.Debug("subscription terminated", "peer", peer.Addr)

	sc.queue = coalesce.NewQueue()
	errChan := make(chan error, 3)
	sc.errChan = errChan

	s.log.Debug("acquiring subscription spot", "Target", sc.target)
	ok := s.subscribeRPCsem.TryAcquire(1)
	if !ok {
		return status.Errorf(codes.ResourceExhausted, "could not acquire a subscription spot")
	}
	s.log.Debug("acquired subscription spot", "Target", sc.target)

	switch sc.req.GetSubscribe().GetMode() {
	case gnmi.SubscriptionList_ONCE:
		go func() {
			s.log.Debug("Handle Subscription", "Mode", "ONCE", "Target", sc.target)
			//s.handleSubscriptionRequest(sc)
			sc.queue.Close()
		}()
	case gnmi.SubscriptionList_POLL:
		go s.log.Debug("Handle Subscription", "Mode", "POLL", "Target", sc.target)
	case gnmi.SubscriptionList_STREAM:
		if sc.req.GetSubscribe().GetUpdatesOnly() {
			sc.queue.Insert(syncMarker{})
		}
		remove := addSubscription(s.m, sc.req.GetSubscribe(), &matchClient{queue: sc.queue})
		defer remove()
		if !sc.req.GetSubscribe().GetUpdatesOnly() {
			s.log.Debug("Handle Subscription", "Mode", "STREAM", "Target", sc.target)
			go s.handleSubscriptionRequest(sc)

		}
	default:
		return status.Errorf(codes.InvalidArgument, "unrecognized subscription mode: %v", sc.req.GetSubscribe().GetMode())
	}
	// send all nodes added to queue
	go s.sendStreamingResults(sc)

	var errs = make([]error, 0)
	for err := range errChan {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		sb := strings.Builder{}
		sb.WriteString("multiple errors occurred:\n")
		for _, err := range errs {
			sb.WriteString(fmt.Sprintf("- %v\n", err))
		}
		return fmt.Errorf("%v", sb)
	}
	return nil
}

type matchClient struct {
	queue *coalesce.Queue
	err   error
}

func (m *matchClient) Update(n interface{}) {
	if m.err != nil {
		return
	}
	_, m.err = m.queue.Insert(n)
}

func addSubscription(m *match.Match, s *gnmi.SubscriptionList, c *matchClient) func() {
	var removes []func()
	prefix := path.ToStrings(s.GetPrefix(), true)
	for _, p := range s.GetSubscription() {
		if p.GetPath() == nil {
			continue
		}

		path := append(prefix, path.ToStrings(p.GetPath(), false)...)
		removes = append(removes, m.AddQuery(path, c))
	}
	return func() {
		for _, remove := range removes {
			remove()
		}
	}
}

func (s *Server) handleSubscriptionRequest(sc *streamClient) {
	var err error
	s.log.Debug("processing subscription", "Target", sc.target)
	defer func() {
		if err != nil {
			s.log.Debug("error processing subscription", "Target", sc.target, "Error", err)
			sc.queue.Close()
			sc.errChan <- err
			return
		}
		s.log.Debug("subscription request processed", "Target", sc.target)
	}()

	if !sc.req.GetSubscribe().GetUpdatesOnly() {
		for _, sub := range sc.req.GetSubscribe().GetSubscription() {
			var fp []string
			fp, err = path.CompletePath(sc.req.GetSubscribe().GetPrefix(), sub.GetPath())
			if err != nil {
				return
			}
			err = s.GetStateCache().GetCache().Query(sc.target, fp,
				func(_ []string, l *ctree.Leaf, _ interface{}) error {
					if err != nil {
						return err
					}
					_, err = sc.queue.Insert(l)
					return nil
				})
			if err != nil {
				s.log.Debug("failed internal cache query", "Target", sc.target, "Error", err)
				return
			}
		}
	}
	_, err = sc.queue.Insert(syncMarker{})
}

func (s *Server) sendStreamingResults(sc *streamClient) {
	ctx := sc.stream.Context()
	peer, _ := peer.FromContext(ctx)
	s.log.Debug("sending streaming results", "Target", sc.target, "Peer", peer.Addr)
	defer s.subscribeRPCsem.Release(1)
	for {
		item, dup, err := sc.queue.Next(ctx)
		if coalesce.IsClosedQueue(err) {
			sc.errChan <- nil
			return
		}
		if err != nil {
			sc.errChan <- err
			return
		}
		if _, ok := item.(syncMarker); ok {
			err = sc.stream.Send(&gnmi.SubscribeResponse{
				Response: &gnmi.SubscribeResponse_SyncResponse{
					SyncResponse: true,
				}})
			if err != nil {
				sc.errChan <- err
				return
			}
			continue
		}
		node, ok := item.(*ctree.Leaf)
		if !ok || node == nil {
			sc.errChan <- status.Errorf(codes.Internal, "invalid cache node: %+v", item)
			return
		}
		err = s.sendSubscribeResponse(&resp{
			stream: sc.stream,
			n:      node,
			dup:    dup,
		}, sc)
		if err != nil {
			s.log.Debug("failed sending subscribeResponse", "target", sc.target, "error", err)
			sc.errChan <- err
			return
		}
		// TODO: check if target was deleted ? necessary ?
	}
}

type resp struct {
	stream gnmi.GNMI_SubscribeServer
	n      *ctree.Leaf
	dup    uint32
}

func (s *Server) sendSubscribeResponse(r *resp, sc *streamClient) error {
	notif, err := subscribe.MakeSubscribeResponse(r.n.Value(), r.dup)
	if err != nil {
		return status.Errorf(codes.Unknown, "unknown error: %v", err)
	}
	// No acls
	return r.stream.Send(notif)
}

// Events for the config cache
func (s *Server) ConfigCacheEvents(n *ctree.Leaf) {
	//s.log.Debug("State CacheUpdates", "Notification", n)
	switch v := n.Value().(type) {
	case *gnmi.Notification:
		//s.log.Debug("State CacheUpdates", "Path", path.ToStrings(v.Prefix, true), "Notification", v)

		// for all the updates check if there is a handler

		subscribe.UpdateNotification(s.m, n, v, path.ToStrings(v.Prefix, true))
	default:
		s.log.Debug("State CacheUpdates unexpected type", "type", reflect.TypeOf(n.Value()))
	}
}

// Events for the state cache
func (s *Server) StateCacheEvents(n *ctree.Leaf) {
	//s.log.Debug("State CacheUpdates", "Notification", n)
	switch v := n.Value().(type) {
	case *gnmi.Notification:
		//s.log.Debug("State CacheUpdates", "Path", path.ToStrings(v.Prefix, true), "Notification", v)

		subscribe.UpdateNotification(s.m, n, v, path.ToStrings(v.Prefix, true))
	default:
		s.log.Debug("State CacheUpdates unexpected type", "type", reflect.TypeOf(n.Value()))
	}
}
