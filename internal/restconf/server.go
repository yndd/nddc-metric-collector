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
package restconf

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/pkg/errors"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/ndd-yang/pkg/cache"
	"github.com/yndd/ndd-yang/pkg/dispatcher"
	"github.com/yndd/ndd-yang/pkg/yentry"

	"github.com/yndd/nddc-metric-collector/internal/controllers/collector"
)

type Config struct {
	// Address
	Address string
}

// Option can be used to manipulate Options.
type Option func(*Server)

// WithLogger specifies how the Reconciler should log messages.
func WithLogger(log logging.Logger) Option {
	return func(s *Server) {
		s.log = log
	}
}

func WithConfig(cfg Config) Option {
	return func(s *Server) {
		s.cfg = cfg
	}
}

func WithConfigCache(c *cache.Cache) Option {
	return func(s *Server) {
		s.configCache = c
	}
}

func WithStateCache(c *cache.Cache) Option {
	return func(s *Server) {
		s.stateCache = c
	}
}

func WithRootSchema(c *yentry.Entry) Option {
	return func(s *Server) {
		s.rootSchema = c
	}
}

func WithRootResource(c dispatcher.Handler) Option {
	return func(s *Server) {
		s.rootResource = c
	}
}

func WithDispatcher(c dispatcher.Dispatcher) Option {
	return func(s *Server) {
		s.dispatcher = c
	}
}

type Server struct {
	cfg Config
	// router
	rootResource dispatcher.Handler
	dispatcher   dispatcher.Dispatcher
	// rootSchema
	rootSchema *yentry.Entry
	// schema
	configCache *cache.Cache
	stateCache  *cache.Cache

	log logging.Logger
}

func New(opts ...Option) (*Server, error) {
	s := &Server{}

	for _, opt := range opts {
		opt(s)
	}

	return s, nil
}

func (s *Server) GetConfigCache() *cache.Cache {
	return s.configCache
}

func (s *Server) GetStateCache() *cache.Cache {
	return s.stateCache
}

func (s *Server) GetRootSchema() *yentry.Entry {
	return s.rootSchema
}

func (s *Server) Run(ctx context.Context) error {
	log := s.log.WithValues("address", s.cfg.Address)
	log.Debug("restconf server run...")
	errChannel := make(chan error)
	go func() {
		if err := s.Serve2(); err != nil {
			errChannel <- errors.Wrap(err, errStartRestConfServer)
		}
		errChannel <- nil
	}()
	return nil
}

func (s *Server) Serve2() error {
	http.HandleFunc("/", s.handleRestconf)
	if err := http.ListenAndServe(s.cfg.Address, nil); err != nil {
		s.log.Debug("Errors", "error", err)
		return errors.Wrap(err, errRestConfServer)
	}
	return nil

}

func (s *Server) Serve() error {

	r := mux.NewRouter()

	r.HandleFunc("/.well-known/host-meta", s.discovery)
	r.HandleFunc("/restconf", s.restconf)
	r.HandleFunc("/restconf/operations", s.operations)
	r.HandleFunc("/restconf/data", s.handleRestconf)
	r.HandleFunc("/restconf/yang-library-version", s.yanginfo)

	if err := http.ListenAndServe(s.cfg.Address, r); err != nil {
		s.log.Debug("Errors", "error", err)
		return errors.Wrap(err, errRestConfServer)
	}
	return nil
}

func (s *Server) discovery(w http.ResponseWriter, r *http.Request) {

}

func (s *Server) restconf(w http.ResponseWriter, r *http.Request) {

}

func (s *Server) operations(w http.ResponseWriter, r *http.Request) {

}

func (s *Server) yanginfo(w http.ResponseWriter, r *http.Request) {

}

func (s *Server) handleRestconf(w http.ResponseWriter, r *http.Request) {
	log := s.log.WithValues(
		"Host", r.URL.Host,
		"Path", r.URL.Path,
		"Content-type", r.Header.Get("Content-type"),
	)
	log.Debug("Data request")
	w.Header().Set("Content-Type", "application/json")

	prefix := &gnmi.Path{Target: collector.GnmiTarget, Origin: collector.GnmiOrigin}
	cache := s.GetStateCache()
	x, err := cache.GetJson(collector.GnmiTarget, prefix, &gnmi.Path{})
	if err != nil {
		log.Debug("Data request", "Error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	json.NewEncoder(w).Encode(x)

}
