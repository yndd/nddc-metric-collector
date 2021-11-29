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
	"context"
	"encoding/json"
	"strconv"
	"time"

	gnmitarget "github.com/karimra/gnmic/target"
	gnmitypes "github.com/karimra/gnmic/types"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/pkg/errors"
	ndrv1 "github.com/yndd/ndd-core/apis/dvr/v1"
	pkgmetav1 "github.com/yndd/ndd-core/apis/pkg/meta/v1"
	"github.com/yndd/ndd-runtime/pkg/event"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/ndd-runtime/pkg/reconciler/managed"
	"github.com/yndd/ndd-runtime/pkg/resource"
	"github.com/yndd/ndd-runtime/pkg/utils"
	"github.com/yndd/ndd-yang/pkg/leafref"
	"github.com/yndd/ndd-yang/pkg/parser"
	"github.com/yndd/ndd-yang/pkg/yentry"
	"github.com/yndd/ndd-yang/pkg/yparser"
	"github.com/yndd/ndd-yang/pkg/yresource"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	cevent "sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	collectorv1alpha1 "github.com/yndd/nddc-metric-collector/apis/collector/v1alpha1"
)

const (
	// Errors
	errUnexpectedCollector       = "the managed resource is not a Collector resource"
	errKubeUpdateFailedCollector = "cannot update Collector"
	errReadCollector             = "cannot read Collector"
	errCreateCollector           = "cannot create Collector"
	erreUpdateCollector          = "cannot update Collector"
	errDeleteCollector           = "cannot delete Collector"

	// resource information
	// resourcePrefixCollector = "collector.nddc.yndd.io.v1alpha1.Collector"
)

// SetupCollector adds a controller that reconciles Collectors.
func SetupCollector(mgr ctrl.Manager, o controller.Options, l logging.Logger, poll time.Duration, namespace string, rs *yentry.Entry) (string, chan cevent.GenericEvent, error) {

	name := managed.ControllerName(collectorv1alpha1.CollectorGroupKind)

	events := make(chan cevent.GenericEvent)

	y := initYangCollector()

	r := managed.NewReconciler(mgr,
		resource.ManagedKind(collectorv1alpha1.CollectorGroupVersionKind),
		managed.WithExternalConnecter(&connectorCollector{
			log:         l,
			kube:        mgr.GetClient(),
			usage:       resource.NewNetworkNodeUsageTracker(mgr.GetClient(), &ndrv1.NetworkNodeUsage{}),
			rootSchema:  rs,
			y:           y,
			newClientFn: gnmitarget.NewTarget},
		),
		managed.WithParser(l),
		managed.WithValidator(&validatorCollector{
			log:        l,
			rootSchema: rs,
			y:          y,
			parser:     *parser.NewParser(parser.WithLogger(l))},
		),
		managed.WithLogger(l.WithValues("controller", name)),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))))

	return collectorv1alpha1.CollectorGroupKind, events, ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o).
		For(&collectorv1alpha1.CollectorCollector{}).
		WithEventFilter(resource.IgnoreUpdateWithoutGenerationChangePredicate()).
		Watches(
			&source.Channel{Source: events},
			&handler.EnqueueRequestForObject{},
		).
		Complete(r)
}

type Collector struct {
	*yresource.Resource
}

func initYangCollector(opts ...yresource.Option) yresource.Handler {
	r := &Collector{&yresource.Resource{}}

	for _, opt := range opts {
		opt(r)
	}
	return r
}

// GetRootPath returns the rootpath of the resource
func (r *Collector) GetRootPath(mg resource.Managed) []*gnmi.Path {
	return []*gnmi.Path{
		{
			Elem: []*gnmi.PathElem{
				{Name: "collector"},
			},
		},
	}
}

// GetParentDependency returns the parent dependency of the resource
func (r *Collector) GetParentDependency(mg resource.Managed) []*leafref.LeafRef {
	rootPath := r.GetRootPath(mg)
	// if the path is not bigger than 1 element there is no parent dependency
	if len(rootPath[0].GetElem()) < 2 {
		return []*leafref.LeafRef{}
	}
	// the dependency path is the rootPath except for the last element
	dependencyPathElem := rootPath[0].GetElem()[:(len(rootPath[0].GetElem()) - 1)]
	// check for keys present, if no keys present we return an empty list
	keysPresent := false
	for _, pathElem := range dependencyPathElem {
		if len(pathElem.GetKey()) != 0 {
			keysPresent = true
		}
	}
	if !keysPresent {
		return []*leafref.LeafRef{}
	}

	// return the rootPath except the last entry
	return []*leafref.LeafRef{
		{
			RemotePath: &gnmi.Path{Elem: dependencyPathElem},
		},
	}
}

type validatorCollector struct {
	log        logging.Logger
	parser     parser.Parser
	rootSchema *yentry.Entry
	y          yresource.Handler
}

func (v *validatorCollector) ValidateLocalleafRef(ctx context.Context, mg resource.Managed) (managed.ValidateLocalleafRefObservation, error) {
	return managed.ValidateLocalleafRefObservation{
		Success:          true,
		ResolvedLeafRefs: []*leafref.ResolvedLeafRef{}}, nil
}

func (v *validatorCollector) ValidateExternalleafRef(ctx context.Context, mg resource.Managed, cfg []byte) (managed.ValidateExternalleafRefObservation, error) {
	log := v.log.WithValues("resource", mg.GetName())
	log.Debug("ValidateExternalleafRef...")

	// json unmarshal the resource
	o, ok := mg.(*collectorv1alpha1.CollectorCollector)
	if !ok {
		return managed.ValidateExternalleafRefObservation{}, errors.New(errUnexpectedCollector)
	}
	d, err := json.Marshal(&o.Spec.ForNetworkNode)
	if err != nil {
		return managed.ValidateExternalleafRefObservation{}, errors.Wrap(err, errJSONMarshal)
	}
	var x1 interface{}
	json.Unmarshal(d, &x1)

	// json unmarshal the external data
	var x2 interface{}
	json.Unmarshal(cfg, &x2)

	rootPath := v.y.GetRootPath(o)

	leafRefs := v.rootSchema.GetLeafRefsLocal(true, rootPath[0], &gnmi.Path{}, make([]*leafref.LeafRef, 0))
	log.Debug("Validate leafRefs ...", "Path", yparser.GnmiPath2XPath(rootPath[0], false), "leafRefs", leafRefs)

	// For local external leafref validation we need to supply the external
	// data to validate the remote leafref, we use x2 for this
	success, resultValidation, err := yparser.ValidateLeafRef(
		rootPath[0], x1, x2, leafRefs, v.rootSchema)
	if err != nil {
		return managed.ValidateExternalleafRefObservation{
			Success: false,
		}, nil
	}
	if !success {
		log.Debug("ValidateExternalleafRef failed", "resultleafRefValidation", resultValidation)
		return managed.ValidateExternalleafRefObservation{
			Success:          false,
			ResolvedLeafRefs: resultValidation}, nil
	}
	log.Debug("ValidateExternalleafRef success", "resultleafRefValidation", resultValidation)
	return managed.ValidateExternalleafRefObservation{
		Success:          true,
		ResolvedLeafRefs: resultValidation}, nil
}

func (v *validatorCollector) ValidateParentDependency(ctx context.Context, mg resource.Managed, cfg []byte) (managed.ValidateParentDependencyObservation, error) {
	log := v.log.WithValues("resource", mg.GetName())
	log.Debug("ValidateParentDependency...")

	o, ok := mg.(*collectorv1alpha1.CollectorCollector)
	if !ok {
		return managed.ValidateParentDependencyObservation{}, errors.New(errUnexpectedCollector)
	}

	dependencyLeafRef := v.y.GetParentDependency(o)

	// unmarshal the config
	var x1 interface{}
	json.Unmarshal(cfg, &x1)
	//log.Debug("Latest Config", "data", x1)

	success, resultValidation, err := yparser.ValidateParentDependency(
		x1, dependencyLeafRef, v.rootSchema)
	if err != nil {
		return managed.ValidateParentDependencyObservation{
			Success: false,
		}, nil
	}
	if !success {
		log.Debug("ValidateParentDependency failed", "resultParentValidation", resultValidation)
		return managed.ValidateParentDependencyObservation{
			Success:          false,
			ResolvedLeafRefs: resultValidation}, nil
	}
	log.Debug("ValidateParentDependency success", "resultParentValidation", resultValidation)
	return managed.ValidateParentDependencyObservation{
		Success:          true,
		ResolvedLeafRefs: resultValidation}, nil
}

// ValidateResourceIndexes validates if the indexes of a resource got changed
// if so we need to delete the original resource, because it will be dangling if we dont delete it
func (v *validatorCollector) ValidateResourceIndexes(ctx context.Context, mg resource.Managed) (managed.ValidateResourceIndexesObservation, error) {
	log := v.log.WithValues("resource", mg.GetName())

	// json unmarshal the resource
	o, ok := mg.(*collectorv1alpha1.CollectorCollector)
	if !ok {
		return managed.ValidateResourceIndexesObservation{}, errors.New(errUnexpectedCollector)
	}
	log.Debug("ValidateResourceIndexes", "Spec", o.Spec)

	rootPath := v.y.GetRootPath(o)

	origResourceIndex := mg.GetResourceIndexes()
	// we call the CompareConfigPathsWithResourceKeys irrespective is the get resource index returns nil
	changed, deletPaths, newResourceIndex := v.parser.CompareGnmiPathsWithResourceKeys(rootPath[0], origResourceIndex)
	if changed {
		log.Debug("ValidateResourceIndexes changed", "deletPaths", deletPaths[0])
		return managed.ValidateResourceIndexesObservation{Changed: true, ResourceDeletes: deletPaths, ResourceIndexes: newResourceIndex}, nil
	}

	log.Debug("ValidateResourceIndexes success")
	return managed.ValidateResourceIndexesObservation{Changed: false, ResourceIndexes: newResourceIndex}, nil
}

// A connector is expected to produce an ExternalClient when its Connect method
// is called.
type connectorCollector struct {
	log         logging.Logger
	kube        client.Client
	usage       resource.Tracker
	rootSchema  *yentry.Entry
	y           yresource.Handler
	newClientFn func(c *gnmitypes.TargetConfig) *gnmitarget.Target
	//newClientFn func(ctx context.Context, cfg ndd.Config) (config.ConfigurationClient, error)
}

// Connect produces an ExternalClient by:
// 1. Tracking that the managed resource is using a NetworkNode.
// 2. Getting the managed resource's NetworkNode with connection details
// A resource is mapped to a single target
func (c *connectorCollector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	log := c.log.WithValues("resource", mg.GetName())
	log.Debug("Connect")

	/*
		o, ok := mg.(*collectorv1alpha1.CollectorCollector)
		if !ok {
			return nil, errors.New(errUnexpectedCollector)
		}
	*/

	cfg := &gnmitypes.TargetConfig{
		Name:       "dummy",
		Address:    pkgmetav1.PrefixService + "-" + "collector" + "." + pkgmetav1.NamespaceLocalK8sDNS + strconv.Itoa((pkgmetav1.GnmiServerPort)),
		Username:   utils.StringPtr("admin"),
		Password:   utils.StringPtr("admin"),
		Timeout:    10 * time.Second,
		SkipVerify: utils.BoolPtr(true),
		Insecure:   utils.BoolPtr(true),
		TLSCA:      utils.StringPtr(""), //TODO TLS
		TLSCert:    utils.StringPtr(""), //TODO TLS
		TLSKey:     utils.StringPtr(""),
		Gzip:       utils.BoolPtr(false),
	}

	cl := gnmitarget.NewTarget(cfg)
	if err := cl.CreateGNMIClient(ctx); err != nil {
		return nil, errors.Wrap(err, errNewClient)
	}

	// we make a string here since we use a trick in registration to go to multiple targets
	// while here the object is mapped to a single target/network node
	tns := []string{"localGNMIServer"}

	/*
		cfgTargets, deletedTargets, err := processTargets(ctx, c.kube, o.Status.Target)
		if err != nil {
			return nil, errors.Wrap(err, "")
		}

		for _, tu := range deletedTargets {
			log.Debug("Stop Subscription", "target", tu.Name)
			c.tuChan <- tu
		}
		tns := []string{}
		for _, tu := range cfgTargets {
			log.Debug("Start Subscription", "target", tu.Name)
			c.tuChan <- tu
			tns = append(tns, tu.Name)
		}
	*/

	return &externalCollector{client: cl, targets: tns, log: log, parser: *parser.NewParser(parser.WithLogger(log)), rootSchema: c.rootSchema, y: c.y}, nil
}

// An ExternalClient observes, then either creates, updates, or deletes an
// external resource to ensure it reflects the managed resource's desired state.
type externalCollector struct {
	//client  config.ConfigurationClient
	client     *gnmitarget.Target
	targets    []string
	log        logging.Logger
	rootSchema *yentry.Entry
	y          yresource.Handler
	parser     parser.Parser
}

func (e *externalCollector) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	o, ok := mg.(*collectorv1alpha1.CollectorCollector)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errUnexpectedCollector)
	}
	log := e.log.WithValues("Resource", o.GetName())
	log.Debug("Observing ...")

	// rootpath of the resource
	rootPath := e.y.GetRootPath(o)
	hierElements := e.rootSchema.GetHierarchicalResourcesLocal(true, rootPath[0], &gnmi.Path{}, make([]*gnmi.Path, 0))
	log.Debug("Observing hierElements ...", "Path", yparser.GnmiPath2XPath(rootPath[0], false), "hierElements", hierElements)

	// gnmi get request
	req := &gnmi.GetRequest{
		Prefix:   &gnmi.Path{Target: GnmiTarget, Origin: GnmiOrigin},
		Path:     rootPath,
		Encoding: gnmi.Encoding_JSON,
		Type:     gnmi.GetRequest_DataType(gnmi.GetRequest_CONFIG),
	}

	// gnmi get response
	resp, err := e.client.Get(ctx, req)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errReadCollector)
	}

	// processObserve
	// o. marshal/unmarshal data
	// 1. check if resource exists
	// 2. remove parent hierarchical elements from spec
	// 3. remove resource hierarchical elements from gnmi response
	// 4. transform the data in gnmi to process the delta
	// 5. find the resource delta: updates and/or deletes in gnmi
	exists, deletes, updates, err := processObserve(rootPath[0], hierElements, &o.Spec.ForNetworkNode, resp, e.rootSchema)
	if err != nil {
		return managed.ExternalObservation{}, err
	}
	if !exists {
		// No Data exists -> Create
		log.Debug("Observing Response:", "Exists", false, "HasData", false, "UpToDate", false, "Response", resp)
		return managed.ExternalObservation{
			Ready:            true,
			ResourceExists:   false,
			ResourceHasData:  false,
			ResourceUpToDate: false,
		}, nil
	}
	// Data exists
	if len(deletes) != 0 || len(updates) != 0 {
		// resource is NOT up to date
		log.Debug("Observing Response: resource NOT up to date", "Exists", true, "HasData", true, "UpToDate", false, "Response", resp, "Updates", updates, "Deletes", deletes)
		for _, del := range deletes {
			log.Debug("Observing Response: resource NOT up to date, deletes", "path", yparser.GnmiPath2XPath(del, true))
		}
		for _, upd := range updates {
			val, _ := e.parser.GetValue(upd.GetVal())
			log.Debug("Observing Response: resource NOT up to date, updates", "path", yparser.GnmiPath2XPath(upd.GetPath(), true), "data", val)
		}
		return managed.ExternalObservation{
			Ready:            true,
			ResourceExists:   true,
			ResourceHasData:  true,
			ResourceUpToDate: false,
			ResourceDeletes:  deletes,
			ResourceUpdates:  updates,
		}, nil
	}
	// resource is up to date
	log.Debug("Observing Response: resource up to date", "Exists", true, "HasData", true, "UpToDate", true, "Response", resp)
	return managed.ExternalObservation{
		Ready:            true,
		ResourceExists:   true,
		ResourceHasData:  true,
		ResourceUpToDate: true,
	}, nil
}

func (e *externalCollector) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	o, ok := mg.(*collectorv1alpha1.CollectorCollector)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errUnexpectedCollector)
	}
	log := e.log.WithValues("Resource", o.GetName())
	log.Debug("Creating ...")

	// get the rootpath of the resource
	rootPath := e.y.GetRootPath(o)

	// processCreate
	// 0. marshal/unmarshal data
	// 1. transform the spec data to gnmi updates
	updates, err := processCreate(rootPath[0], &o.Spec.ForNetworkNode, e.rootSchema)
	for _, update := range updates {
		log.Debug("Create Fine Grane Updates", "Path", yparser.GnmiPath2XPath(update.Path, true), "Value", update.GetVal())
	}

	if len(updates) == 0 {
		log.Debug("cannot create object since there are no updates present")
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateObject)
	}

	req := &gnmi.SetRequest{
		Prefix:  &gnmi.Path{Target: GnmiTarget, Origin: GnmiOrigin},
		Replace: updates,
	}

	_, err = e.client.Set(ctx, req)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateCollector)
	}

	return managed.ExternalCreation{}, nil
}

func (e *externalCollector) Update(ctx context.Context, mg resource.Managed, obs managed.ExternalObservation) (managed.ExternalUpdate, error) {
	o, ok := mg.(*collectorv1alpha1.CollectorCollector)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errUnexpectedCollector)
	}
	log := e.log.WithValues("Resource", o.GetName())
	log.Debug("Updating ...")

	for _, u := range obs.ResourceUpdates {
		log.Debug("Update -> Update", "Path", u.Path, "Value", u.GetVal())
	}
	for _, d := range obs.ResourceDeletes {
		log.Debug("Update -> Delete", "Path", d)
	}

	req := &gnmi.SetRequest{
		Prefix: &gnmi.Path{Target: GnmiTarget, Origin: GnmiOrigin},
		Update: obs.ResourceUpdates,
		Delete: obs.ResourceDeletes,
	}

	_, err := e.client.Set(ctx, req)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errReadCollector)
	}

	return managed.ExternalUpdate{}, nil
}

func (e *externalCollector) Delete(ctx context.Context, mg resource.Managed) error {
	o, ok := mg.(*collectorv1alpha1.CollectorCollector)
	if !ok {
		return errors.New(errUnexpectedCollector)
	}
	log := e.log.WithValues("Resource", o.GetName())
	log.Debug("Deleting ...")

	// get the rootpath of the resource
	rootPath := e.y.GetRootPath(o)

	req := gnmi.SetRequest{
		Prefix: &gnmi.Path{Target: GnmiTarget, Origin: GnmiOrigin},
		Delete: rootPath,
	}

	_, err := e.client.Set(ctx, &req)
	if err != nil {
		return errors.Wrap(err, errDeleteCollector)
	}

	return nil
}

func (e *externalCollector) GetTarget() []string {
	return e.targets
}

func (e *externalCollector) GetConfig(ctx context.Context) ([]byte, error) {
	e.log.Debug("Get Config ...")
	req := &gnmi.GetRequest{
		Prefix:   &gnmi.Path{Target: GnmiTarget, Origin: GnmiOrigin},
		Path:     []*gnmi.Path{},
		Encoding: gnmi.Encoding_JSON,
		Type:     gnmi.GetRequest_DataType(gnmi.GetRequest_CONFIG),
	}

	resp, err := e.client.Get(ctx, req)
	if err != nil {
		return make([]byte, 0), errors.Wrap(err, errGetConfig)
	}

	if len(resp.GetNotification()) != 0 {
		if len(resp.GetNotification()[0].GetUpdate()) != 0 {
			x2, err := e.parser.GetValue(resp.GetNotification()[0].GetUpdate()[0].Val)
			if err != nil {
				return make([]byte, 0), errors.Wrap(err, errGetConfig)
			}

			data, err := json.Marshal(x2)
			if err != nil {
				return make([]byte, 0), errors.Wrap(err, errJSONMarshal)
			}
			return data, nil
		}
	}
	e.log.Debug("Get Config Empty response")
	return nil, nil
}

func (e *externalCollector) GetResourceName(ctx context.Context, path []*gnmi.Path) (string, error) {
	return "", nil
}
