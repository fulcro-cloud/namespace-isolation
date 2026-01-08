package plugin

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

var NamespaceQuotaGVR = schema.GroupVersionResource{
	Group:    "brasa.cloud",
	Version:  "v1alpha1",
	Resource: "namespacequotas",
}

// QuotaCache maintains an in-memory map of namespaces with active quotas,
// synchronized via a Kubernetes informer watching NamespaceQuota resources.
type QuotaCache struct {
	mu     sync.RWMutex
	quotas map[string]bool

	client   dynamic.Interface
	informer cache.SharedIndexInformer
	stopCh   chan struct{}
	log      *logrus.Entry
}

func NewQuotaCache(kubeconfig string, log *logrus.Entry) (*QuotaCache, error) {
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	qc := &QuotaCache{
		quotas: make(map[string]bool),
		client: dynamicClient,
		stopCh: make(chan struct{}),
		log:    log.WithField("component", "cache"),
	}

	factory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 30*time.Second)
	qc.informer = factory.ForResource(NamespaceQuotaGVR).Informer()

	_, err = qc.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    qc.onAdd,
		UpdateFunc: qc.onUpdate,
		DeleteFunc: qc.onDelete,
	})
	if err != nil {
		return nil, err
	}

	return qc, nil
}

func (qc *QuotaCache) Start(ctx context.Context) error {
	qc.log.Info("Starting quota cache")

	if err := qc.initialSync(); err != nil {
		qc.log.WithError(err).Warn("Initial sync failed, continuing with empty cache")
	}

	go qc.informer.Run(qc.stopCh)

	if !cache.WaitForCacheSync(ctx.Done(), qc.informer.HasSynced) {
		qc.log.Warn("Cache sync timed out")
	}

	return nil
}

func (qc *QuotaCache) Stop() {
	qc.log.Debug("Stopping quota cache")
	close(qc.stopCh)
}

func (qc *QuotaCache) HasQuota(namespace string) bool {
	qc.mu.RLock()
	defer qc.mu.RUnlock()
	return qc.quotas[namespace]
}

func (qc *QuotaCache) GetNamespaces() []string {
	qc.mu.RLock()
	defer qc.mu.RUnlock()

	namespaces := make([]string, 0, len(qc.quotas))
	for ns := range qc.quotas {
		namespaces = append(namespaces, ns)
	}
	return namespaces
}

func (qc *QuotaCache) initialSync() error {
	list, err := qc.client.Resource(NamespaceQuotaGVR).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	qc.mu.Lock()
	defer qc.mu.Unlock()

	for _, item := range list.Items {
		ns := qc.extractNamespace(&item)
		if ns != "" && qc.isEnabled(&item) {
			qc.quotas[ns] = true
		}
	}

	qc.log.WithField("count", len(qc.quotas)).Info("Initial sync complete")
	return nil
}

func (qc *QuotaCache) onAdd(obj interface{}) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return
	}

	ns := qc.extractNamespace(u)
	if ns == "" || !qc.isEnabled(u) {
		return
	}

	qc.mu.Lock()
	qc.quotas[ns] = true
	qc.mu.Unlock()

	qc.log.WithField("namespace", ns).Info("Quota added")
}

func (qc *QuotaCache) onUpdate(_, newObj interface{}) {
	u, ok := newObj.(*unstructured.Unstructured)
	if !ok {
		return
	}

	ns := qc.extractNamespace(u)
	if ns == "" {
		return
	}

	enabled := qc.isEnabled(u)

	qc.mu.Lock()
	if enabled {
		qc.quotas[ns] = true
	} else {
		delete(qc.quotas, ns)
	}
	qc.mu.Unlock()

	qc.log.WithFields(logrus.Fields{
		"namespace": ns,
		"enabled":   enabled,
	}).Debug("Quota updated")
}

func (qc *QuotaCache) onDelete(obj interface{}) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		u, ok = tombstone.Obj.(*unstructured.Unstructured)
		if !ok {
			return
		}
	}

	ns := qc.extractNamespace(u)
	if ns == "" {
		return
	}

	qc.mu.Lock()
	delete(qc.quotas, ns)
	qc.mu.Unlock()

	qc.log.WithField("namespace", ns).Info("Quota removed")
}

func (qc *QuotaCache) extractNamespace(u *unstructured.Unstructured) string {
	spec, found, err := unstructured.NestedMap(u.Object, "spec")
	if err != nil || !found {
		return ""
	}

	ns, found, err := unstructured.NestedString(spec, "namespace")
	if err != nil || !found {
		return ""
	}

	return ns
}

func (qc *QuotaCache) isEnabled(u *unstructured.Unstructured) bool {
	spec, found, err := unstructured.NestedMap(u.Object, "spec")
	if err != nil || !found {
		return true
	}

	enabled, found, err := unstructured.NestedBool(spec, "enabled")
	if err != nil || !found {
		return true
	}

	return enabled
}
