// Package kube provides Kubernetes pod listing and metadata extraction.
package kube

import (
	"context"
	"log"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ClusterMode determines which types of GKE nodes to include.
type ClusterMode string

const (
	// ClusterModeAutopilot includes only Autopilot nodes (gk3- prefix).
	ClusterModeAutopilot ClusterMode = "autopilot"
	// ClusterModeStandard includes only Standard nodes (gke- prefix).
	ClusterModeStandard ClusterMode = "standard"
	// ClusterModeAll includes both Autopilot and Standard nodes.
	ClusterModeAll ClusterMode = "all"
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
	NodeName        string // Kubernetes node name
	IsAutopilot     bool   // true if pod is on an Autopilot node (gk3- prefix)
}

// PodLister lists pods from a Kubernetes cluster.
type PodLister struct {
	client            kubernetes.Interface
	namespace         string
	excludeNamespaces map[string]bool
	clusterMode       ClusterMode
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

// WithClusterMode sets the cluster mode for filtering pods by node type.
func WithClusterMode(mode ClusterMode) PodListerOption {
	return func(pl *PodLister) { pl.clusterMode = mode }
}

// WithExcludeNamespaces sets namespaces to exclude from pod listing.
func WithExcludeNamespaces(ns []string) PodListerOption {
	return func(pl *PodLister) {
		pl.excludeNamespaces = make(map[string]bool, len(ns))
		for _, n := range ns {
			if n != "" {
				pl.excludeNamespaces[n] = true
			}
		}
	}
}

// NewPodLister creates a PodLister using the default kubeconfig.
func NewPodLister(opts ...PodListerOption) (*PodLister, error) {
	pl := &PodLister{
		namespace:   "", // all namespaces
		clusterMode: ClusterModeAll,
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
		p := &podList.Items[i]
		if !pl.includeNode(p.Spec.NodeName, p.Labels) {
			continue
		}
		if pl.excludeNamespaces[p.Namespace] {
			continue
		}
		pods = append(pods, extractPodInfo(p))
	}
	return pods, nil
}

// includeNode returns true if the pod's node should be included based on the cluster mode.
// Detection uses the node name prefix as the primary signal (gk3- for Autopilot, gke- for Standard)
// and falls back to pod labels (cloud.google.com/gke-nodepool or autopilot.gke.io/).
func (pl *PodLister) includeNode(nodeName string, podLabels map[string]string) bool {
	ap := isAutopilotNode(nodeName) || hasAutopilotLabels(podLabels)
	std := isStandardNode(nodeName) || (!ap && hasNodePoolLabel(podLabels))

	switch pl.clusterMode {
	case ClusterModeAutopilot:
		return ap
	case ClusterModeStandard:
		return std
	case ClusterModeAll:
		if !ap && !std && nodeName != "" {
			log.Printf("Warning: pod on node %q does not match autopilot (gk3-) or standard (gke-) prefix; skipping", nodeName)
		}
		return ap || std
	default:
		return ap || std
	}
}

// hasAutopilotLabels returns true if the pod's labels indicate it was scheduled by Autopilot.
func hasAutopilotLabels(labels map[string]string) bool {
	for k := range labels {
		if strings.HasPrefix(k, "autopilot.gke.io/") {
			return true
		}
	}
	return false
}

// hasNodePoolLabel returns true if the pod has a GKE node pool label,
// indicating it's running on a managed GKE node.
func hasNodePoolLabel(labels map[string]string) bool {
	_, ok := labels["cloud.google.com/gke-nodepool"]
	return ok
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
		NodeName:        pod.Spec.NodeName,
		IsAutopilot:     isAutopilotNode(pod.Spec.NodeName) || hasAutopilotLabels(pod.Labels),
	}
}

// isAutopilotNode returns true if the node name indicates a GKE Autopilot node.
// Autopilot nodes use the "gk3-" prefix, while Standard nodes use "gke-".
func isAutopilotNode(nodeName string) bool {
	return strings.HasPrefix(nodeName, "gk3-")
}

// isStandardNode returns true if the node name indicates a GKE Standard node.
func isStandardNode(nodeName string) bool {
	return strings.HasPrefix(nodeName, "gke-")
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
		IsAutopilot:     true, // default to autopilot for backward compat
	}
}

// NewTestPodInfoOnNode creates a PodInfo for testing with an explicit node name.
func NewTestPodInfoOnNode(name, namespace string, cpuMilli int64, memMB int64, startTime time.Time, isSpot bool, labels map[string]string, nodeName string) PodInfo {
	pi := NewTestPodInfo(name, namespace, cpuMilli, memMB, startTime, isSpot, labels)
	pi.NodeName = nodeName
	pi.IsAutopilot = isAutopilotNode(nodeName)
	return pi
}
