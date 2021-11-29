package applogic

import (
	"context"
	"time"

	"github.com/karimra/gnmic/types"
	"github.com/pkg/errors"
	ndrv1 "github.com/yndd/ndd-core/apis/dvr/v1"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/ndd-runtime/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	errGetNetworkNode = "cannot get NetworkNode"
	reconcileTimer    = 5 * time.Second
)

type targetInfo struct {
	config       []*types.TargetConfig
	observerList []TargetObserver
	client       client.Client
	log          logging.Logger
	stopCh       chan bool
}

// Option can be used to manipulate Options.
type TargetOption func(*targetInfo)

// WithLogger specifies how the Reconciler should log messages.
func WithLogger(log logging.Logger) TargetOption {
	return func(s *targetInfo) {
		s.log = log
	}
}

func WithK8sClient(c client.Client) TargetOption {
	return func(s *targetInfo) {
		s.client = c
	}
}

func NewTarget(opts ...TargetOption) *targetInfo {
	x := &targetInfo{
		config: []*types.TargetConfig{},
		/*
			config: []*types.TargetConfig{
				{
					Name:       "leaf1",
					Address:    "172.20.20.2:57400",
					Username:   utils.StringPtr("admin"),
					Password:   utils.StringPtr("admin"),
					Timeout:    10 * time.Second,
					SkipVerify: utils.BoolPtr(true),
					Insecure:   utils.BoolPtr(false),
					TLSCA:      utils.StringPtr(""), //TODO TLS
					TLSCert:    utils.StringPtr(""), //TODO TLS
					TLSKey:     utils.StringPtr(""), //TODO TLS
					Gzip:       utils.BoolPtr(false),
				},
				{
					Name:       "leaf2",
					Address:    "172.20.20.4:57400",
					Username:   utils.StringPtr("admin"),
					Password:   utils.StringPtr("admin"),
					Timeout:    10 * time.Second,
					SkipVerify: utils.BoolPtr(true),
					Insecure:   utils.BoolPtr(false),
					TLSCA:      utils.StringPtr(""), //TODO TLS
					TLSCert:    utils.StringPtr(""), //TODO TLS
					TLSKey:     utils.StringPtr(""), //TODO TLS
					Gzip:       utils.BoolPtr(false),
				},
			},
		*/
		observerList: []TargetObserver{},
	}

	for _, opt := range opts {
		opt(x)
	}

	x.stopCh = make(chan bool)
	//defer close(x.stopCh)
	// start reconcile process
	go func() {
		x.targetWatcher()
	}()
	return x
}

func (t *targetInfo) GetTargets() []*types.TargetConfig {
	return t.config
}

func (t *targetInfo) RegisterTargetObserver(o TargetObserver) {
	t.observerList = append(t.observerList, o)
}

func (t *targetInfo) RemoveTargetObserver(o TargetObserver) {
	found := false
	i := 0
	for ; i < len(t.observerList); i++ {
		if t.observerList[i] == o {
			found = true
			break
		}
	}
	if found {
		t.observerList = append(t.observerList[:i], t.observerList[i+1:]...)
	}
}

func (t *targetInfo) NotifyTargetObserver() {
	for _, observer := range t.observerList {
		observer.handleTargetUpdate(t.config)
	}
}

type TargetSubject interface {
	RegisterTargetObserver(o TargetObserver)
	RemoveTargetObserver(o TargetObserver)
	NotifyTargetObserver()
}

type TargetObserver interface {
	handleTargetUpdate([]*types.TargetConfig)
}

func (t *targetInfo) targetWatcher() error {
	t.log.Debug("start target watcher")
	timeout := make(chan bool, 1)
	timeout <- true
	for {
		select {
		case <-timeout:
			time.Sleep(reconcileTimer)
			timeout <- true

			// get targets
			nnl := &ndrv1.NetworkNodeList{}
			if err := t.client.List(context.Background(), nnl); err != nil {
				return errors.Wrap(err, errGetNetworkNode)
			}

			// find all targets that have are in configured status
			targets := []*types.TargetConfig{}
			for _, nn := range nnl.Items {
				if nn.GetCondition(ndrv1.ConditionKindDeviceDriverConfigured).Status == corev1.ConditionTrue {
					targets = append(targets, &types.TargetConfig{
						Name:       nn.GetName(),
						Address:    nn.GetTargetAddress(),
						Username:   utils.StringPtr("admin"),
						Password:   utils.StringPtr("admin"),
						Timeout:    10 * time.Second,
						SkipVerify: utils.BoolPtr(true),
						Insecure:   utils.BoolPtr(false),
						TLSCA:      utils.StringPtr(""), //TODO TLS
						TLSCert:    utils.StringPtr(""), //TODO TLS
						TLSKey:     utils.StringPtr(""), //TODO TLS
						Gzip:       utils.BoolPtr(false),
					})
				}
			}
			t.config = targets
			for _, o := range t.observerList {
				//t.log.Debug("Active targets", "Targets", ts)
				o.handleTargetUpdate(t.config)
			}

		case <-t.stopCh:
			t.log.Debug("Stopping target watcher process")
			return nil
		}
	}
}
