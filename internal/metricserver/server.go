package metricserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/ndd-yang/pkg/cache"
	"github.com/yndd/ndd-yang/pkg/dispatcher"
	"github.com/yndd/ndd-yang/pkg/yentry"
	"github.com/yndd/ndd-yang/pkg/yparser"
	collectorv1alpha1 "github.com/yndd/nddc-metric-collector/apis/collector/v1alpha1"
	"github.com/yndd/nddc-metric-collector/internal/controllers/collector"
)

const (
	metricNameRegex = "[^a-zA-Z0-9_]+"
	retryInterval   = 2 * time.Second
)

type server struct {
	address string
	//caches
	configCache *cache.Cache
	stateCache  *cache.Cache
	targetCache *cache.Cache
	// router
	rootResource dispatcher.Handler
	// rootSchema
	rootSchema *yentry.Entry

	srv         *http.Server
	srvCancelFn context.CancelFunc
	regCancelFn context.CancelFunc
	//
	metricRegex *regexp.Regexp

	log logging.Logger
}

// Option can be used to manipulate Options.
type Option func(*server)

// WithLogger specifies how the Reconciler should log messages.
func WithLogger(log logging.Logger) Option {
	return func(s *server) {
		s.log = log
	}
}

// WithLogger specifies how the Reconciler should log messages.
func WithAddress(a string) Option {
	return func(s *server) {
		s.address = a
	}
}

func WithRootSchema(c *yentry.Entry) Option {
	return func(s *server) {
		s.rootSchema = c
	}
}

func WithRootResource(c dispatcher.Handler) Option {
	return func(s *server) {
		s.rootResource = c
	}
}

func WithConfigCache(c *cache.Cache) Option {
	return func(s *server) {
		s.configCache = c
	}
}

func WithStateCache(c *cache.Cache) Option {
	return func(s *server) {
		s.stateCache = c
	}
}

func WithTargetCache(c *cache.Cache) Option {
	return func(s *server) {
		s.targetCache = c
	}
}

func New(opts ...Option) (Server, error) {
	s := &server{
		metricRegex: regexp.MustCompile(metricNameRegex),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s, nil
}

func (s *server) getConfigCache() *cache.Cache {
	return s.configCache
}

func (s *server) getStateCache() *cache.Cache {
	return s.stateCache
}

func (s *server) getTargetCache() *cache.Cache {
	return s.targetCache
}

func (s *server) getRootSchema() *yentry.Entry {
	return s.rootSchema
}

type Server interface {
	Start(ctx context.Context)
}

func (s *server) Start(ctx context.Context) {
	sctx, cancel := context.WithCancel(ctx)
	s.srvCancelFn = cancel

START:
	select {
	case <-sctx.Done():
		s.log.Debug("server context Done", "Error", sctx.Err())
		return
	default:
		registry := prometheus.NewRegistry()
		err := registry.Register(s)
		if err != nil {
			s.log.Debug("failed to add exporter to prometheus registry", "Error", err)
			time.Sleep(retryInterval)
			goto START
		}
		// create http server
		promHandler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{ErrorHandling: promhttp.ContinueOnError})
		mux := http.NewServeMux()
		mux.Handle("/metrics", promHandler)
		mux.Handle("/", new(healthHandler))

		s.srv = &http.Server{Addr: s.address, Handler: mux}

		listener, err := net.Listen("tcp", s.address)
		if err != nil {
			s.log.Debug("failed to create tcp listener", "Error", err)
			time.Sleep(retryInterval)
			goto START
		}
		// start http server
		s.log.Debug("starting http server...", "Address", s.srv.Addr)

		go func() {
			err = s.srv.Serve(listener)
			if err != nil && err != http.ErrServerClosed {
				s.log.Debug("prometheus server error:", "Error", err)
			}
			s.log.Debug("http server closed...")
		}()

	}
}

// Describe implements prometheus.Collector
func (s *server) Describe(ch chan<- *prometheus.Desc) {}

// Collect implements prometheus.Collector
func (s *server) Collect(ch chan<- prometheus.Metric) {
	log := s.log.WithValues("Name", "prometheus metric-collector")
	log.Debug("prometheus collect ...")
	// get collector config
	c, err := s.getCollectorConfig()
	if err != nil {
		log.Debug("prometheus collect get config", "rrror", err)
		return
	}
	// get paths
	metrics := c.GetMetrics()

	// get targets
	for _, t := range s.rootResource.GetTargets() {
		targetName := t.Name

		if len(metrics) == 0 {
			// no metrics present so we can stop
			break
		}
		if !s.getTargetCache().GetCache().HasTarget(targetName) {
			log.Debug("prometheus collect", "error", "target not found in cache", "target", targetName)
			break
		}
		cache := s.getTargetCache()

		wg1 := new(sync.WaitGroup)
		wg1.Add(len(metrics))
		for name, m := range metrics {
			go func(name string, m *collectorv1alpha1.NddccollectorCollectorMetric) {
				defer wg1.Done()
				wg2 := new(sync.WaitGroup)
				wg2.Add(len(m.Paths))
				for _, path := range m.Paths {
					go func(p *string) {
						defer wg2.Done()
						//log.Debug("prometheus collect collecting metric", "name", name, "path", *p)

						ns, err := cache.QueryAll(t.Name, &gnmi.Path{Target: targetName}, yparser.Xpath2GnmiPath(*p, 0))
						if err != nil {
							log.Debug("prometheus collect get Information From cache", "Error", err, "Target", targetName)
							return
						}
						if ns != nil {
							wg3 := new(sync.WaitGroup)
							wg3.Add(len(ns))
							for _, n := range ns {
								go func(n *gnmi.Notification) {
									defer wg3.Done()
									wg4 := new(sync.WaitGroup)
									wg4.Add(len(n.GetUpdate()))
									for _, u := range n.GetUpdate() {
										go func(u *gnmi.Update) {
											defer wg4.Done()
											var prefix string
											if m.Prefix == nil {
												prefix = ""
											} else {
												prefix = *m.Prefix
											}
											name, vname, labels, labelValues := s.getLabels(prefix, targetName, u.GetPath())

											val, err := yparser.GetValue(u.GetVal())
											if err != nil {
												log.Debug("get value from update", "error", err, "target", targetName)
												return
											}
											v, err := getFloat(val)
											if err != nil {
												log.Debug("getfloat from value", "error", err, "target", targetName)
												return
											}

											//log.Debug("prometheus collect data from cache", "name", name, "vname", vname, "labels", labels, "labelValues", labelValues)

											ch <- prometheus.MustNewConstMetric(
												prometheus.NewDesc(s.metricName(name, vname), *m.Description, labels, nil),
												prometheus.UntypedValue,
												v,
												labelValues...)
										}(u)
									}
									wg4.Wait()
								}(n)
							}
							wg3.Wait()
						}
					}(path)
				}
				wg2.Wait()
			}(name, m)
		}
		wg1.Wait()
	}
}

func (s *server) metricName(name, valueName string) string {
	// more customizations ?
	valueName = fmt.Sprintf("%s_%s", name, path.Base(valueName))
	return strings.TrimLeft(s.metricRegex.ReplaceAllString(valueName, "_"), "_")
}

func (s *server) getCollectorConfig() (*collectorv1alpha1.NddccollectorCollector, error) {
	prefix := &gnmi.Path{Target: collector.GnmiTarget, Origin: collector.GnmiOrigin}
	path := &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "collector"}}}
	x, err := s.getConfigCache().GetJson(collector.GnmiTarget, prefix, path)
	if err != nil {
		return nil, err
	}

	if x == nil {
		// empty response
		return &collectorv1alpha1.NddccollectorCollector{}, nil
	}
	d, err := json.Marshal(x)
	if err != nil {
		return nil, err
	}
	c := &collectorv1alpha1.NddccollectorCollector{}
	if err := json.Unmarshal(d, c); err != nil {
		return nil, err
	}

	return c, nil
}

func (s *server) getLabels(prefix string, t string, p *gnmi.Path) (string, string, []string, []string) {
	// name of the last element of the path
	vname := s.metricRegex.ReplaceAllString(filepath.Base(yparser.GnmiPath2XPath(p, true)), "_")
	name := ""

	// all the keys of the path
	labels := []string{"target_name"}
	// all the values of the keys in the path
	labelValues := []string{t}
	for i, pathElem := range p.GetElem() {
		// prometheus names should all be _ based
		//promName := strings.ReplaceAll(pathElem.GetName(), "-", "_")
		pathElemPromName := s.metricRegex.ReplaceAllString(pathElem.GetName(), "_")
		if i == 0 {
			if prefix != "" {
				name = strings.Join([]string{prefix, pathElemPromName}, "_")
			} else {
				name = pathElemPromName
			}

		} else if i < len(p.GetElem())-1 {
			name = strings.Join([]string{name, pathElemPromName}, "_")
		}
		if len(pathElem.GetKey()) != 0 {
			for k, v := range pathElem.GetKey() {
				// append the keyName of the path

				labels = append(labels, strings.Join([]string{pathElemPromName, s.metricRegex.ReplaceAllString(k, "_")}, "_"))
				//labels = append(labels, pathElemPromName+"_"+s.metricRegex.ReplaceAllString(k, "_"))
				// append the values of the keys
				labelValues = append(labelValues, v)
			}
		}
	}
	return name, vname, labels, labelValues

}

func getFloat(v interface{}) (float64, error) {
	switch i := v.(type) {
	case float64:
		return float64(i), nil
	case float32:
		return float64(i), nil
	case int64:
		return float64(i), nil
	case int32:
		return float64(i), nil
	case int16:
		return float64(i), nil
	case int8:
		return float64(i), nil
	case uint64:
		return float64(i), nil
	case uint32:
		return float64(i), nil
	case uint16:
		return float64(i), nil
	case uint8:
		return float64(i), nil
	case int:
		return float64(i), nil
	case uint:
		return float64(i), nil
	case string:
		f, err := strconv.ParseFloat(i, 64)
		if err != nil {
			return math.NaN(), err
		}
		return f, err
	default:
		return math.NaN(), errors.New("getFloat: unknown value is of incompatible type")
	}
}
