// Package kube provides Kubernetes pod listing and metadata extraction.
package kube

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// PodInfo contains the extracted pod data needed for cost calculation.
type PodInfo struct {
	Name            string
	Namespace       string
	Labels          map[string]string
	CPURequestMilli int64   // millicores
	MemRequestBytes int64   // bytes
	CPURequestVCPU  float64 // vCPU (derived from milli)
	MemRequestGB    float64 // GB (derived from bytes)
	StartTime       time.Time
	IsSpot          bool
	Phase           corev1.PodPhase
}

// PodLister lists pods from a Kubernetes cluster.
type PodLister struct {
	client    kubernetes.Interface
	namespace string
}

// PodListerOption configures a PodLister.
type PodListerOption func(*PodLister)

// WithNamespace restricts pod listing to a specific namespace. Empty means all namespaces.
func WithNamespace(ns string) PodListerOption {
	return func(pl *PodLister) { pl.namespace = ns }
}

// WithClient sets a custom kubernetes client (for testing).
func WithClient(c kubernetes.Interface) PodListerOption {
	return func(pl *PodLister) { pl.client = c }
}

// NewPodLister creates a PodLister using the default kubeconfig.
func NewPodLister(opts ...PodListerOption) (*PodLister, error) {
	pl := &PodLister{
		namespace: "", // all namespaces
	}
	for _, opt := range opts {
		opt(pl)
	}

	if pl.client == nil {
		client, err := buildClient()
		if err != nil {
			return nil, err
		}
		pl.client = client
	}

	return pl, nil
}

func buildClient() (kubernetes.Interface, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		config, err = kubeConfig.ClientConfig()
		if err != nil {
			return nil, err
		}
	}
	return kubernetes.NewForConfig(config)
}

// ListPods returns PodInfo for all running pods.
func (pl *PodLister) ListPods(ctx context.Context) ([]PodInfo, error) {
	podList, err := pl.client.CoreV1().Pods(pl.namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return nil, err
	}

	pods := make([]PodInfo, 0, len(podList.Items))
	for i := range podList.Items {
		pods = append(pods, extractPodInfo(&podList.Items[i]))
	}
	return pods, nil
}

func extractPodInfo(pod *corev1.Pod) PodInfo {
	var cpuMilli int64
	var memBytes int64

	for _, c := range pod.Spec.Containers {
		if req, ok := c.Resources.Requests[corev1.ResourceCPU]; ok {
			cpuMilli += req.MilliValue()
		}
		if req, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
			memBytes += req.Value()
		}
	}

	var startTime time.Time
	if pod.Status.StartTime != nil {
		startTime = pod.Status.StartTime.Time
	}

	return PodInfo{
		Name:            pod.Name,
		Namespace:       pod.Namespace,
		Labels:          pod.Labels,
		CPURequestMilli: cpuMilli,
		MemRequestBytes: memBytes,
		CPURequestVCPU:  float64(cpuMilli) / 1000.0,
		MemRequestGB:    float64(memBytes) / 1e9,
		StartTime:       startTime,
		IsSpot:          isSpotPod(pod),
		Phase:           pod.Status.Phase,
	}
}

// isSpotPod detects whether a pod is running on GKE Autopilot Spot.
func isSpotPod(pod *corev1.Pod) bool {
	ns := pod.Spec.NodeSelector
	if ns == nil {
		return false
	}

	if ns["cloud.google.com/gke-spot"] == "true" {
		return true
	}
	if ns["cloud.google.com/compute-class"] == "autopilot-spot" {
		return true
	}
	return false
}

// NewTestPodInfo creates a PodInfo for testing purposes.
// memMB is in megabytes (1 MB = 1,000,000 bytes) to match billing units (GB = 10^9 bytes).
func NewTestPodInfo(name, namespace string, cpuMilli int64, memMB int64, startTime time.Time, isSpot bool, labels map[string]string) PodInfo {
	memBytes := memMB * 1_000_000
	return PodInfo{
		Name:            name,
		Namespace:       namespace,
		Labels:          labels,
		CPURequestMilli: cpuMilli,
		MemRequestBytes: memBytes,
		CPURequestVCPU:  float64(cpuMilli) / 1000.0,
		MemRequestGB:    float64(memBytes) / 1e9,
		StartTime:       startTime,
		IsSpot:          isSpot,
		Phase:           corev1.PodRunning,
	}
}
