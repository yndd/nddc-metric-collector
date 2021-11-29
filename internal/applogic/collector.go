package applogic

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/karimra/gnmic/types"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/pkg/errors"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/ndd-yang/pkg/cache"
	"github.com/yndd/ndd-yang/pkg/dispatcher"
	"github.com/yndd/ndd-yang/pkg/yentry"
	"github.com/yndd/ndd-yang/pkg/yparser"
	collectorv1alpha1 "github.com/yndd/nddc-metric-collector/apis/collector/v1alpha1"
	"github.com/yndd/nddc-metric-collector/internal/metriccollector"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	collectorDummyName = "dummy"
)

type collector struct {
	dispatcher.Resource
	data             *collectorv1alpha1.NddccollectorCollector
	parent           *root
	activeTargets    []*types.TargetConfig // active targets
	target           *targetInfo           // observer interface
	metriccollectors map[string]metriccollector.MetricCollector
}

func (r *collector) WithLogging(log logging.Logger) {
	r.Log = log
}

func (r *collector) WithStateCache(c *cache.Cache) {
	r.StateCache = c
}

func (r *collector) WithTargetCache(c *cache.Cache) {
	r.TargetCache = c
}

func (r *collector) WithConfigCache(c *cache.Cache) {
	r.ConfigCache = c
}

func (r *collector) WithPrefix(p *gnmi.Path) {
	r.Prefix = p
}

func (r *collector) WithPathElem(pe []*gnmi.PathElem) {
	r.PathElem = pe[0]
}

func (r *collector) WithRootSchema(rs *yentry.Entry) {
	r.RootSchema = rs
}

func (r *collector) WithK8sClient(c client.Client) {
	r.Client = c
}

func NewCollector(n string, opts ...dispatcher.Option) dispatcher.Handler {
	x := &collector{
		metriccollectors: make(map[string]metriccollector.MetricCollector),
	}
	x.Key = n

	for _, opt := range opts {
		opt(x)
	}
	return x
}

func collectorGetKey(p []*gnmi.PathElem) string {
	return collectorDummyName
}

func collectorCreate(log logging.Logger, cc, sc, tc *cache.Cache, c client.Client, prefix *gnmi.Path, p []*gnmi.PathElem, d interface{}) dispatcher.Handler {
	collectorName := collectorGetKey(p)
	return NewCollector(collectorName,
		dispatcher.WithPrefix(prefix),
		dispatcher.WithPathElem(p),
		dispatcher.WithLogging(log),
		dispatcher.WithStateCache(sc),
		dispatcher.WithTargetCache(tc),
		dispatcher.WithConfigCache(cc),
		dispatcher.WithK8sClient(c))
}

func (r *collector) HandleConfigEvent(o dispatcher.Operation, prefix *gnmi.Path, pe []*gnmi.PathElem, d interface{}) (dispatcher.Handler, error) {
	log := r.Log.WithValues("Operation", o, "Path Elem", pe)

	log.Debug("collector Handle")

	if len(pe) == 1 {
		return nil, errors.New("the handle should have been terminated in the parent")
	} else {
		return nil, errors.New("there is no children in the collector resource ")
	}
}

func (r *collector) CreateChild(children map[string]dispatcher.HandleConfigEventFunc, pathElemName string, prefix *gnmi.Path, pe []*gnmi.PathElem, d interface{}) (dispatcher.Handler, error) {
	return nil, errors.New("there is no children in the collector resource ")
}

func (r *collector) DeleteChild(pathElemName string, pe []*gnmi.PathElem) error {
	return errors.New("there is no children in the collector resource ")
}

func (r *collector) SetParent(parent interface{}) error {
	p, ok := parent.(*root)
	if !ok {
		return errors.New("wrong parent object")
	}
	r.parent = p
	return nil
}

func (r *collector) SetRootSchema(rs *yentry.Entry) {
	r.RootSchema = rs
}

func (r *collector) GetChildren() map[string]string {
	x := make(map[string]string)
	return x
}

func (r *collector) UpdateConfig(d interface{}) error {
	r.Copy(d)
	// from the moment we get dat we are interested in target updates
	r.GetTarget().RegisterTargetObserver(r)
	// add channel here
	r.handleConfigUpdate()
	//r.data.SetLastUpdate(time.Now().String())
	// update the state cache
	//if err := r.UpdateStateCache(); err != nil {
	//	return err
	//}
	return nil
}

func (r *collector) GetPathElem(p []*gnmi.PathElem, do_recursive bool) ([]*gnmi.PathElem, error) {
	r.Log.Debug("GetPathElem", "PathElem collector", r.PathElem)
	return []*gnmi.PathElem{r.PathElem}, nil
}

func (r *collector) Copy(d interface{}) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	x := collectorv1alpha1.NddccollectorCollector{}
	if err := json.Unmarshal(b, &x); err != nil {
		return err
	}
	r.data = (&x).DeepCopy()
	r.Log.Debug("Copy", "Data", r.data)
	return nil
}

func (r *collector) UpdateStateCache() error {
	pe, err := r.GetPathElem(nil, true)
	if err != nil {
		return err
	}
	b, err := json.Marshal(r.data)
	if err != nil {
		return err
	}
	var x interface{}
	if err := json.Unmarshal(b, &x); err != nil {
		return err
	}
	//log.Debug("Debug updateState", "refPaths", refPaths)
	r.Log.Debug("Debug updateState", "data", x)
	u, err := yparser.GetGranularUpdatesFromJSON(&gnmi.Path{Elem: pe}, x, r.RootSchema)
	n := &gnmi.Notification{
		Timestamp: time.Now().UnixNano(),
		Prefix:    r.Prefix,
		Update:    u,
	}
	//n, err := r.StateCache.GetNotificationFromJSON2(r.Prefix, &gnmi.Path{Elem: pe}, x, r.RootSchema)
	if err != nil {
		return err
	}
	if u != nil {
		if err := r.StateCache.GnmiUpdate(r.Prefix.Target, n); err != nil {
			if strings.Contains(fmt.Sprintf("%v", err), "stale") {
				return nil
			}
			return err
		}
	}
	return nil
}

func (r *collector) DeleteStateCache() error {
	// delete all the metriccollectors that are active
	r.handleDelete()

	pe, err := r.GetPathElem(nil, true)
	if err != nil {
		return err
	}
	n := &gnmi.Notification{
		Timestamp: time.Now().UnixNano(),
		Prefix:    r.Prefix,
		Delete:    []*gnmi.Path{{Elem: pe}},
	}
	if err := r.StateCache.GnmiUpdate(r.Prefix.Target, n); err != nil {
		return err
	}
	return nil
}

func (r *collector) GetTarget() *targetInfo {
	return r.parent.GetTarget()
}

func (r *collector) GetTargets() []*types.TargetConfig {
	return r.parent.GetTargets()
}

func (r *collector) handleDelete() {
	r.Log.Debug("handleDelete", "Targets", r.GetTargets(), "Data", r.data)
	for _, t := range r.GetTargets() {
		// stop and start the collectors
		if mc, ok := r.metriccollectors[t.Name]; ok {
			// metric collector exists stop and start
			mc.Stop()
			delete(r.metriccollectors, t.Name)

			r.Log.Debug("handleDelete stopped metric collector", "Target", t.Name, "Data", r.data)
		}
	}
}

func (r *collector) handleConfigUpdate() {
	r.Log.Debug("handleConfigUpdate", "Targets", r.GetTargets(), "Data", r.data)
	// get active targets
	for _, t := range r.GetTargets() {
		if !r.TargetCache.GetCache().HasTarget(t.Name) {
			r.TargetCache.GetCache().Add(t.Name)
		}
		// stop and start the collectors
		if mc, ok := r.metriccollectors[t.Name]; ok {
			// metric collector exists stop and start
			mc.Stop()
			delete(r.metriccollectors, t.Name)

			r.Log.Debug("handleConfigUpdate stopped metric collector", "Target", t.Name, "Data", r.data)
		}
		// create metric collector
		var err error
		r.metriccollectors[t.Name], err = metriccollector.New(t, r.data,
			metriccollector.WithLogger(r.Log),
			metriccollector.WithTargetCache(r.TargetCache),
		)
		r.Log.Debug("handleConfigUpdate create new metric collector", "Target", t.Name, "Data", r.data)
		if err != nil {
			r.Log.Debug("Metriccollector create failed", "Error", err)
			return
		}
		r.metriccollectors[t.Name].Start()

	}
}

func (r *collector) handleTargetUpdate(targets []*types.TargetConfig) {
	// check for deleted targets
	for _, at := range r.activeTargets {
		log := r.Log.WithValues("Target", at.Name)
		found := false
		for _, t := range targets {
			if at.Name == t.Name {
				found = true
				log.Debug("handleTargetUpdate target running")
				break
			}
		}
		if !found {
			log.Debug("handleTargetUpdate target delete")
			// stop metriccollector
			if mc, ok := r.metriccollectors[at.Name]; ok {
				mc.Stop()
				delete(r.metriccollectors, at.Name)
			}
			if r.TargetCache.GetCache().HasTarget(at.Name) {
				r.TargetCache.GetCache().Remove(at.Name)
			}
		}
	}

	// validate new targets
	for _, t := range targets {
		log := r.Log.WithValues("Target", t.Name)
		found := false
		for _, at := range r.activeTargets {
			if at.Name == t.Name {
				found = true
				break
			}
		}
		if !found {
			log.Debug("handleTargetUpdate target new")
			// add target to the cache
			if !r.TargetCache.GetCache().HasTarget(t.Name) {
				r.TargetCache.GetCache().Add(t.Name)
			}
			// start metriccollector -> new target
			var err error
			r.metriccollectors[t.Name], err = metriccollector.New(t, r.data,
				metriccollector.WithLogger(r.Log),
				metriccollector.WithTargetCache(r.TargetCache),
			)
			r.Log.Debug("handleTargetUpdate create new metric collector", "Target", t.Name, "Data", r.data)
			if err != nil {
				r.Log.Debug("Metriccollector create failed", "Error", err)
				return
			}
			r.metriccollectors[t.Name].Start()
		}
	}
	r.activeTargets = targets
}
