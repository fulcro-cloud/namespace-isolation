package agent

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
)

const eventComponentName = "namespace-isolator"

type K8sClient struct {
	dynamicClient dynamic.Interface
	clientset     kubernetes.Interface
	recorder      record.EventRecorder
}

func NewK8sClient(kubeconfig string) (*K8sClient, error) {
	config, err := buildConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	recorder := createEventRecorder(clientset)

	return &K8sClient{
		dynamicClient: dynamicClient,
		clientset:     clientset,
		recorder:      recorder,
	}, nil
}

func buildConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build config from kubeconfig: %w", err)
		}
		return config, nil
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}
	return config, nil
}

func createEventRecorder(clientset kubernetes.Interface) record.EventRecorder {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: clientset.CoreV1().Events(""),
	})
	return eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{
		Component: eventComponentName,
	})
}

func (c *K8sClient) GetDynamicClient() dynamic.Interface {
	return c.dynamicClient
}

func (c *K8sClient) GetClientset() kubernetes.Interface {
	return c.clientset
}

func (c *K8sClient) GetEventRecorder() record.EventRecorder {
	return c.recorder
}

func (c *K8sClient) GetNamespaceQuotaResource() dynamic.ResourceInterface {
	return c.dynamicClient.Resource(NamespaceQuotaGVR)
}

func (c *K8sClient) EmitEvent(namespace, eventType, reason, message string) {
	ref := &corev1.ObjectReference{
		Kind:      "Namespace",
		Name:      namespace,
		Namespace: namespace,
	}
	c.recorder.Event(ref, eventType, reason, message)
}

func (c *K8sClient) EmitEventForObject(obj *unstructured.Unstructured, eventType, reason, message string) {
	ref := &corev1.ObjectReference{
		APIVersion: obj.GetAPIVersion(),
		Kind:       obj.GetKind(),
		Name:       obj.GetName(),
		Namespace:  obj.GetNamespace(),
		UID:        obj.GetUID(),
	}
	c.recorder.Event(ref, eventType, reason, message)
}

func (c *K8sClient) UpdateStatus(ctx context.Context, name string, ready bool, message string) error {
	resource := c.GetNamespaceQuotaResource()

	obj, err := resource.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get NamespaceQuota %s: %w", name, err)
	}

	status := map[string]interface{}{
		"ready":       ready,
		"message":     message,
		"lastUpdated": time.Now().UTC().Format(time.RFC3339),
	}

	if err := unstructured.SetNestedMap(obj.Object, status, "status"); err != nil {
		return fmt.Errorf("failed to set status: %w", err)
	}

	_, err = resource.UpdateStatus(ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update status for %s: %w", name, err)
	}

	return nil
}
