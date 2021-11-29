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
	"net"
	"strings"

	"github.com/openconfig/gnmi/match"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/pkg/errors"
	"github.com/yndd/ndd-runtime/pkg/logging"
	ynddparser "github.com/yndd/ndd-yang/pkg/parser"
	"golang.org/x/sync/semaphore"
	"google.golang.org/grpc"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/yndd/ndd-yang/pkg/cache"
	"github.com/yndd/ndd-yang/pkg/dispatcher"
	"github.com/yndd/ndd-yang/pkg/yentry"

	collectorv1alpha1 "github.com/yndd/nddc-metric-collector/apis/collector/v1alpha1"
	"github.com/yndd/nddc-metric-collector/internal/controllers/collector"
	"github.com/yndd/nddc-metric-collector/internal/kapi"
)

const (
	// defaults
	defaultMaxSubscriptions = 64
	defaultMaxGetRPC        = 64
)

type Config struct {
	// Address
	Address string
	// Generic
	MaxSubscriptions int64
	MaxUnaryRPC      int64
	// TLS
	InSecure   bool
	SkipVerify bool
	CaFile     string
	CertFile   string
	KeyFile    string
	// observability
	EnableMetrics bool
	Debug         bool
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

func WithKapi(a *kapi.Kapi) Option {
	return func(s *Server) {
		s.client = a
	}
}

func WithEventChannels(e map[string]chan event.GenericEvent) Option {
	return func(s *Server) {
		s.EventChannels = e
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

func WithTargetCache(c *cache.Cache) Option {
	return func(s *Server) {
		s.targetCache = c
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
	gnmi.UnimplementedGNMIServer
	cfg Config

	// kubernetes
	client        *kapi.Kapi
	EventChannels map[string]chan event.GenericEvent

	// router
	rootResource dispatcher.Handler
	dispatcher   dispatcher.Dispatcher
	// rootSchema
	rootSchema *yentry.Entry
	// schema
	configCache *cache.Cache
	stateCache  *cache.Cache
	targetCache *cache.Cache
	m           *match.Match // only used for statecache for now -> TBD if we need to make this more
	//schemaRaw interface{}
	//schema    *collectorv1alpha1.Nddccollector
	// gnmi calls
	subscribeRPCsem *semaphore.Weighted
	unaryRPCsem     *semaphore.Weighted
	// logging and parsing
	parser *ynddparser.Parser
	//handler *Handler
	log logging.Logger

	// context
	ctx context.Context
}

func New(opts ...Option) (*Server, error) {
	s := &Server{
		m: match.New(),
	}

	for _, opt := range opts {
		opt(s)
	}

	// set cache event handlers
	s.GetConfigCache().GetCache().SetClient(s.ConfigCacheEvents)
	s.GetStateCache().GetCache().SetClient(s.StateCacheEvents)

	s.ctx = context.Background()

	// get the original status from k8s
	if err := s.GetInitialState(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Server) GetConfigCache() *cache.Cache {
	return s.configCache
}

func (s *Server) GetStateCache() *cache.Cache {
	return s.stateCache
}

func (s *Server) GetTargetCache() *cache.Cache {
	return s.targetCache
}

func (s *Server) GetRootSchema() *yentry.Entry {
	return s.rootSchema
}

func (s *Server) Run(ctx context.Context) error {
	log := s.log.WithValues("grpcServerAddress", s.cfg.Address)
	log.Debug("grpc server run...")
	errChannel := make(chan error)
	go func() {
		if err := s.Start(); err != nil {
			errChannel <- errors.Wrap(err, errStartGRPCServer)
		}
		errChannel <- nil
	}()
	return nil
}

// Start GRPC Server
func (s *Server) Start() error {
	s.subscribeRPCsem = semaphore.NewWeighted(defaultMaxSubscriptions)
	s.unaryRPCsem = semaphore.NewWeighted(defaultMaxGetRPC)
	log := s.log.WithValues("grpcServerAddress", s.cfg.Address)
	log.Debug("grpc server start...")

	// create a listener on a specific address:port
	l, err := net.Listen("tcp", s.cfg.Address)
	if err != nil {
		return errors.Wrap(err, errCreateTcpListener)
	}

	// TODO, proper handling of the certificates with CERT Manager
	/*
		opts, err := s.serverOpts()
		if err != nil {
			return err
		}
	*/
	// create a gRPC server object
	grpcServer := grpc.NewServer()

	// attach the gnmi service to the grpc server
	gnmi.RegisterGNMIServer(grpcServer, s)

	// start the server
	log.Debug("grpc server serve...")
	if err := grpcServer.Serve(l); err != nil {
		s.log.Debug("Errors", "error", err)
		return errors.Wrap(err, errGrpcServer)
	}
	return nil
}

func (s *Server) GetInitialState() error {
	Nddccollector, err := s.client.ListNddccollector(s.ctx)
	if err != nil {
		return err
	}
	b, err := json.Marshal(Nddccollector)
	if err != nil {
		return err
	}
	var x interface{}
	if err := json.Unmarshal(b, &x); err != nil {
		return err
	}

	s.log.Debug("GetInitialState", "Nddccollector", x)

	prefix := &gnmi.Path{Target: collector.GnmiTarget, Origin: collector.GnmiOrigin}

	n, err := s.GetStateCache().GetNotificationFromJSON2(
		prefix,
		&gnmi.Path{},
		x,
		s.GetRootSchema())
	if err != nil {
		return err
	}
	if n != nil {
		if err := s.GetStateCache().GnmiUpdate(prefix.Target, n); err != nil {
			if strings.Contains(fmt.Sprintf("%v", err), "stale") {
				return nil
			}
			return err
		}
	}
	return nil
}

func (s *Server) GetConfig() (*collectorv1alpha1.Nddccollector, error) {
	prefix := &gnmi.Path{Target: collector.GnmiTarget, Origin: collector.GnmiOrigin}
	x, err := s.GetConfigCache().GetJson(collector.GnmiTarget, prefix, &gnmi.Path{Elem: []*gnmi.PathElem{}})
	if err != nil {
		return nil, err
	}
	s.log.Debug("GetConfig", "config", x)
	b, err := json.Marshal(x)
	if err != nil {
		return nil, err
	}
	n := collectorv1alpha1.Nddccollector{}
	if err := json.Unmarshal(b, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *Server) GetState() (*collectorv1alpha1.Nddccollector, error) {
	prefix := &gnmi.Path{Target: collector.GnmiTarget, Origin: collector.GnmiOrigin}
	x, err := s.GetStateCache().GetJson(collector.GnmiTarget, prefix, &gnmi.Path{Elem: []*gnmi.PathElem{}})
	if err != nil {
		return nil, err
	}
	s.log.Debug("GetState", "state", x)
	b, err := json.Marshal(x)
	if err != nil {
		return nil, err
	}
	n := collectorv1alpha1.Nddccollector{}
	if err := json.Unmarshal(b, &n); err != nil {
		return nil, err
	}
	return &n, nil
}
