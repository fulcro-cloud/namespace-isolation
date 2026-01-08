package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	maxRetries   = 5
	resyncPeriod = 30 * time.Second

	reasonCgroupConfigured = "CgroupConfigured"
	reasonCgroupFailed     = "CgroupFailed"
	reasonCgroupRemoved    = "CgroupRemoved"
	reasonQuotaDisabled    = "QuotaDisabled"
)

type ControllerConfig struct {
	Kubeconfig    string
	CgroupRoot    string
	SlicePrefix   string
	Log           *logrus.Logger
	MetricsServer *MetricsServer
}

type Controller struct {
	k8sClient     *K8sClient
	cgroupManager *CgroupManager
	metricsServer *MetricsServer
	informer      cache.SharedIndexInformer
	workqueue     workqueue.TypedRateLimitingInterface[string]
	log           *logrus.Logger
}

func NewController(config ControllerConfig) (*Controller, error) {
	k8sClient, err := NewK8sClient(config.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	cgroupManager := NewCgroupManager(config.CgroupRoot, config.SlicePrefix, config.Log)

	rateLimiter := workqueue.DefaultTypedControllerRateLimiter[string]()
	queue := workqueue.NewTypedRateLimitingQueue(rateLimiter)

	resource := k8sClient.GetDynamicClient().Resource(NamespaceQuotaGVR)
	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return resource.List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return resource.Watch(context.Background(), options)
			},
		},
		&unstructured.Unstructured{},
		resyncPeriod,
		cache.Indexers{},
	)

	controller := &Controller{
		k8sClient:     k8sClient,
		cgroupManager: cgroupManager,
		metricsServer: config.MetricsServer,
		informer:      informer,
		workqueue:     queue,
		log:           config.Log,
	}

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.onAdd,
		UpdateFunc: controller.onUpdate,
		DeleteFunc: controller.onDelete,
	})

	return controller, nil
}

func (c *Controller) Run(ctx context.Context) error {
	defer c.workqueue.ShutDown()

	c.log.Info("Starting controller")

	go c.informer.Run(ctx.Done())

	c.log.Info("Waiting for informer cache to sync")
	if !cache.WaitForCacheSync(ctx.Done(), c.informer.HasSynced) {
		return fmt.Errorf("failed to sync informer cache")
	}
	c.log.Info("Informer cache synced")

	c.log.Info("Starting worker")
	go c.runWorker(ctx)

	<-ctx.Done()
	c.log.Info("Shutting down controller")

	return nil
}

func (c *Controller) runWorker(ctx context.Context) {
	for c.processNextItem(ctx) {
	}
}

func (c *Controller) processNextItem(ctx context.Context) bool {
	key, shutdown := c.workqueue.Get()
	if shutdown {
		return false
	}
	defer c.workqueue.Done(key)

	err := c.reconcile(ctx, key)
	if err == nil {
		c.workqueue.Forget(key)
		return true
	}

	if c.workqueue.NumRequeues(key) < maxRetries {
		c.log.WithFields(logrus.Fields{
			"key":     key,
			"error":   err,
			"retries": c.workqueue.NumRequeues(key),
		}).Warn("Error processing item, retrying")
		c.workqueue.AddRateLimited(key)
		return true
	}

	c.log.WithFields(logrus.Fields{
		"key":   key,
		"error": err,
	}).Error("Max retries exceeded, dropping item")
	c.workqueue.Forget(key)

	return true
}

func (c *Controller) reconcile(ctx context.Context, key string) error {
	log := c.log.WithField("key", key)
	log.Debug("Reconciling NamespaceQuota")

	obj, exists, err := c.informer.GetStore().GetByKey(key)
	if err != nil {
		return fmt.Errorf("failed to get object from cache: %w", err)
	}

	if !exists {
		log.Info("NamespaceQuota deleted, removing cgroup")
		return c.handleDelete(key)
	}

	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("unexpected object type: %T", obj)
	}

	spec, err := ParseNamespaceQuota(u)
	if err != nil {
		log.WithError(err).Error("Failed to parse NamespaceQuota")
		c.updateStatus(ctx, u.GetName(), false, fmt.Sprintf("Parse error: %v", err))
		c.k8sClient.EmitEventForObject(u, corev1.EventTypeWarning, reasonCgroupFailed,
			fmt.Sprintf("Failed to parse NamespaceQuota: %v", err))
		return err
	}

	return c.handleQuota(ctx, u, spec)
}

func (c *Controller) handleQuota(ctx context.Context, obj *unstructured.Unstructured, spec *NamespaceQuotaSpec) error {
	name := obj.GetName()
	log := c.log.WithFields(logrus.Fields{
		"name":      name,
		"namespace": spec.Namespace,
		"cpu":       spec.CPU,
		"memory":    spec.Memory,
		"enabled":   spec.Enabled,
	})

	if !spec.Enabled {
		log.Info("Quota disabled, removing cgroup if exists")
		if err := c.cgroupManager.RemoveSlice(spec.Namespace); err != nil {
			log.WithError(err).Warn("Failed to remove cgroup slice")
		}
		c.updateStatus(ctx, name, true, "Quota disabled")
		c.k8sClient.EmitEventForObject(obj, corev1.EventTypeNormal, reasonQuotaDisabled,
			"Quota disabled, cgroup removed")
		return nil
	}

	log.Info("Ensuring cgroup slice")
	if err := c.cgroupManager.EnsureSlice(spec.Namespace, spec.CPU, spec.Memory); err != nil {
		log.WithError(err).Error("Failed to ensure cgroup slice")
		c.updateStatus(ctx, name, false, fmt.Sprintf("Cgroup error: %v", err))
		c.k8sClient.EmitEventForObject(obj, corev1.EventTypeWarning, reasonCgroupFailed,
			fmt.Sprintf("Failed to configure cgroup: %v", err))
		return err
	}

	c.updateStatus(ctx, name, true, "Cgroup configured successfully")
	c.k8sClient.EmitEventForObject(obj, corev1.EventTypeNormal, reasonCgroupConfigured,
		fmt.Sprintf("Cgroup configured with CPU=%s, Memory=%s", spec.CPU, spec.Memory))

	c.updateMetrics(spec)

	return nil
}

func (c *Controller) updateMetrics(spec *NamespaceQuotaSpec) {
	if c.metricsServer == nil {
		return
	}

	stats, err := c.metricsServer.ReadCgroupStats(spec.Namespace)
	if err != nil {
		c.log.WithError(err).Debug("Failed to read cgroup stats for metrics")
		return
	}

	var cpuLimitUsec, memoryLimitBytes int64
	if spec.CPU != "" {
		cpuLimitUsec, _ = ParseCPU(spec.CPU)
	}
	if spec.Memory != "" {
		memoryLimitBytes, _ = ParseMemory(spec.Memory)
	}

	c.metricsServer.UpdateMetrics(spec.Namespace, stats, cpuLimitUsec, memoryLimitBytes)
}

func (c *Controller) handleDelete(name string) error {
	c.log.WithField("name", name).Info("Attempting to remove cgroup for deleted quota")

	if err := c.cgroupManager.RemoveSlice(name); err != nil {
		c.log.WithError(err).Warn("Failed to remove cgroup slice on delete")
	} else {
		c.k8sClient.EmitEvent(name, corev1.EventTypeNormal, reasonCgroupRemoved,
			fmt.Sprintf("Cgroup removed for deleted NamespaceQuota %s", name))
	}

	return nil
}

func (c *Controller) updateStatus(ctx context.Context, name string, ready bool, message string) {
	log := c.log.WithFields(logrus.Fields{
		"name":    name,
		"ready":   ready,
		"message": message,
	})

	if err := c.k8sClient.UpdateStatus(ctx, name, ready, message); err != nil {
		log.WithError(err).Warn("Failed to update status")
	} else {
		log.Debug("Status updated")
	}
}

func (c *Controller) onAdd(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		c.log.WithError(err).Error("Failed to get key for add event")
		return
	}
	c.log.WithField("key", key).Debug("Add event received")
	c.workqueue.Add(key)
}

func (c *Controller) onUpdate(oldObj, newObj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(newObj)
	if err != nil {
		c.log.WithError(err).Error("Failed to get key for update event")
		return
	}
	c.log.WithField("key", key).Debug("Update event received")
	c.workqueue.Add(key)
}

func (c *Controller) onDelete(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		c.log.WithError(err).Error("Failed to get key for delete event")
		return
	}
	c.log.WithField("key", key).Debug("Delete event received")
	c.workqueue.Add(key)
}
