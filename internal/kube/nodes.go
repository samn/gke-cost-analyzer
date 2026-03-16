package kube

import (
	"context"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// NodeInfo contains the extracted node data needed for standard GKE cost attribution.
type NodeInfo struct {
	Name          string
	MachineType   string  // from label: node.kubernetes.io/instance-type (e.g., "n2-standard-4")
	MachineFamily string  // parsed from MachineType (e.g., "n2")
	VCPU          float64 // from node.Status.Allocatable cpu
	MemoryGB      float64 // from node.Status.Allocatable memory (bytes / 1e9)
	IsSpot        bool    // from label: cloud.google.com/gke-spot=true
}

// NodeLister lists nodes from a Kubernetes cluster.
type NodeLister struct {
	client kubernetes.Interface
}

// NodeListerOption configures a NodeLister.
type NodeListerOption func(*NodeLister)

// WithNodeClient sets a custom kubernetes client (for testing).
func WithNodeClient(c kubernetes.Interface) NodeListerOption {
	return func(nl *NodeLister) { nl.client = c }
}

// NewNodeLister creates a NodeLister using the default kubeconfig.
func NewNodeLister(opts ...NodeListerOption) (*NodeLister, error) {
	nl := &NodeLister{}
	for _, opt := range opts {
		opt(nl)
	}

	if nl.client == nil {
		client, err := buildClient()
		if err != nil {
			return nil, err
		}
		nl.client = client
	}

	return nl, nil
}

// ListNodes returns NodeInfo for all nodes in the cluster.
func (nl *NodeLister) ListNodes(ctx context.Context) ([]NodeInfo, error) {
	nodeList, err := nl.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	nodes := make([]NodeInfo, 0, len(nodeList.Items))
	for i := range nodeList.Items {
		node := &nodeList.Items[i]

		// Only include standard GKE nodes
		if !isStandardNode(node.Name) {
			continue
		}

		machineType := node.Labels["node.kubernetes.io/instance-type"]
		family := parseMachineFamily(machineType)

		var vcpu float64
		if cpuQ, ok := node.Status.Allocatable["cpu"]; ok {
			vcpu = float64(cpuQ.MilliValue()) / 1000.0
		}

		var memGB float64
		if memQ, ok := node.Status.Allocatable["memory"]; ok {
			memGB = float64(memQ.Value()) / 1e9
		}

		isSpot := node.Labels["cloud.google.com/gke-spot"] == "true"

		nodes = append(nodes, NodeInfo{
			Name:          node.Name,
			MachineType:   machineType,
			MachineFamily: family,
			VCPU:          vcpu,
			MemoryGB:      memGB,
			IsSpot:        isSpot,
		})
	}
	return nodes, nil
}

// parseMachineFamily extracts the machine family from a GCE machine type string.
// Examples: "n2-standard-4" → "n2", "e2-medium" → "e2", "custom-4-8192" → "n1".
func parseMachineFamily(machineType string) string {
	if machineType == "" {
		return ""
	}

	parts := strings.SplitN(machineType, "-", 2)
	if len(parts) == 0 {
		return ""
	}

	family := strings.ToLower(parts[0])

	// Bare "custom-N-M" machine types are billed at N1 rates.
	if family == "custom" {
		return "n1"
	}

	return family
}
