package kube

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestListNodes(t *testing.T) {
	nodes := []runtime.Object{
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gke-cluster-pool-abc123",
				Labels: map[string]string{
					"node.kubernetes.io/instance-type": "n2-standard-4",
				},
			},
			Status: corev1.NodeStatus{
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3920m"),
					corev1.ResourceMemory: resource.MustParse("14Gi"),
				},
			},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gke-cluster-spot-def456",
				Labels: map[string]string{
					"node.kubernetes.io/instance-type": "e2-medium",
					"cloud.google.com/gke-spot":        "true",
				},
			},
			Status: corev1.NodeStatus{
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("940m"),
					corev1.ResourceMemory: resource.MustParse("3Gi"),
				},
			},
		},
		// Autopilot node — should be excluded
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gk3-cluster-autopilot-xyz",
				Labels: map[string]string{
					"node.kubernetes.io/instance-type": "e2-medium",
				},
			},
			Status: corev1.NodeStatus{
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
			},
		},
	}

	client := fake.NewSimpleClientset(nodes...)
	nl, err := NewNodeLister(WithNodeClient(client))
	if err != nil {
		t.Fatal(err)
	}

	result, err := nl.ListNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 standard nodes, got %d", len(result))
	}

	// Find nodes by name
	nodeMap := make(map[string]NodeInfo)
	for _, n := range result {
		nodeMap[n.Name] = n
	}

	n2 := nodeMap["gke-cluster-pool-abc123"]
	if n2.MachineType != "n2-standard-4" {
		t.Errorf("n2 machine type = %s, want n2-standard-4", n2.MachineType)
	}
	if n2.MachineFamily != "n2" {
		t.Errorf("n2 family = %s, want n2", n2.MachineFamily)
	}
	if n2.VCPU != 3.92 {
		t.Errorf("n2 VCPU = %f, want 3.92", n2.VCPU)
	}
	expectedMemGB := float64(14*1024*1024*1024) / 1e9
	if n2.MemoryGB != expectedMemGB {
		t.Errorf("n2 MemoryGB = %f, want %f", n2.MemoryGB, expectedMemGB)
	}
	if n2.IsSpot {
		t.Error("n2 should not be spot")
	}

	e2 := nodeMap["gke-cluster-spot-def456"]
	if e2.MachineFamily != "e2" {
		t.Errorf("e2 family = %s, want e2", e2.MachineFamily)
	}
	if !e2.IsSpot {
		t.Error("e2 should be spot")
	}
	if e2.VCPU != 0.94 {
		t.Errorf("e2 VCPU = %f, want 0.94", e2.VCPU)
	}
}

func TestListNodesAPIError(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "nodes", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("API server unavailable")
	})

	nl, err := NewNodeLister(WithNodeClient(client))
	if err != nil {
		t.Fatal(err)
	}

	_, err = nl.ListNodes(context.Background())
	if err == nil {
		t.Fatal("expected error from API failure")
	}
}

func TestListNodesEmpty(t *testing.T) {
	client := fake.NewSimpleClientset()
	nl, err := NewNodeLister(WithNodeClient(client))
	if err != nil {
		t.Fatal(err)
	}

	result, err := nl.ListNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 nodes, got %d", len(result))
	}
}

func TestListNodesZeroAllocatable(t *testing.T) {
	// Node with zero allocatable resources should still be included
	// (with a log warning), not silently dropped.
	nodes := []runtime.Object{
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gke-cluster-pool-zerores",
				Labels: map[string]string{
					"node.kubernetes.io/instance-type": "n2-standard-4",
				},
			},
			Status: corev1.NodeStatus{
				Allocatable: corev1.ResourceList{
					// No CPU or memory set → zero values
				},
			},
		},
	}

	client := fake.NewSimpleClientset(nodes...)
	nl, err := NewNodeLister(WithNodeClient(client))
	if err != nil {
		t.Fatal(err)
	}

	result, err := nl.ListNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 node (with zero resources), got %d", len(result))
	}

	n := result[0]
	if n.VCPU != 0 {
		t.Errorf("VCPU = %f, want 0", n.VCPU)
	}
	if n.MemoryGB != 0 {
		t.Errorf("MemoryGB = %f, want 0", n.MemoryGB)
	}
	if n.MachineFamily != "n2" {
		t.Errorf("family = %s, want n2", n.MachineFamily)
	}
}

func TestListNodesMissingLabels(t *testing.T) {
	// Node without instance-type label should get empty machine type/family.
	nodes := []runtime.Object{
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "gke-cluster-pool-nolabel",
				Labels: map[string]string{},
			},
			Status: corev1.NodeStatus{
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
			},
		},
	}

	client := fake.NewSimpleClientset(nodes...)
	nl, err := NewNodeLister(WithNodeClient(client))
	if err != nil {
		t.Fatal(err)
	}

	result, err := nl.ListNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result))
	}

	if result[0].MachineType != "" {
		t.Errorf("machine type = %s, want empty", result[0].MachineType)
	}
	if result[0].MachineFamily != "" {
		t.Errorf("machine family = %s, want empty", result[0].MachineFamily)
	}
	if result[0].IsSpot {
		t.Error("should not be spot without the label")
	}
}

func TestParseMachineFamily(t *testing.T) {
	tests := []struct {
		machineType    string
		expectedFamily string
	}{
		{"n2-standard-4", "n2"},
		{"e2-medium", "e2"},
		{"n1-standard-8", "n1"},
		{"c3-standard-4", "c3"},
		{"c3d-standard-8", "c3d"},
		{"t2d-standard-4", "t2d"},
		{"t2a-standard-4", "t2a"},
		{"n2d-standard-4", "n2d"},
		{"n4-standard-4", "n4"},
		{"c4-standard-4", "c4"},
		{"m3-megamem-64", "m3"},
		{"a2-highgpu-1g", "a2"},
		{"g2-standard-4", "g2"},
		{"custom-4-8192", "n1"},    // bare custom → n1
		{"n2-custom-4-8192", "n2"}, // family-prefixed custom
		{"e2-custom-2-4096", "e2"}, // family-prefixed custom
		{"", ""},                   // empty
	}

	for _, tt := range tests {
		t.Run(tt.machineType, func(t *testing.T) {
			got := parseMachineFamily(tt.machineType)
			if got != tt.expectedFamily {
				t.Errorf("parseMachineFamily(%q) = %q, want %q", tt.machineType, got, tt.expectedFamily)
			}
		})
	}
}
