package kube

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestListPods(t *testing.T) {
	startTime := metav1.NewTime(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC))

	pods := []runtime.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "web-abc",
				Namespace: "default",
				Labels: map[string]string{
					"app":  "web",
					"team": "platform",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "web",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
						},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase:     corev1.PodRunning,
				StartTime: &startTime,
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "worker-xyz",
				Namespace: "batch",
				Labels: map[string]string{
					"app":  "worker",
					"team": "data",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "main",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
					},
					{
						Name: "sidecar",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("250m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				},
				NodeSelector: map[string]string{
					"cloud.google.com/gke-spot": "true",
				},
			},
			Status: corev1.PodStatus{
				Phase:     corev1.PodRunning,
				StartTime: &startTime,
			},
		},
	}

	client := fake.NewSimpleClientset(pods...)
	pl, err := NewPodLister(WithClient(client))
	if err != nil {
		t.Fatal(err)
	}

	result, err := pl.ListPods(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 pods, got %d", len(result))
	}

	// Find pod by name
	var web, worker PodInfo
	for _, p := range result {
		switch p.Name {
		case "web-abc":
			web = p
		case "worker-xyz":
			worker = p
		}
	}

	// Verify web pod
	if web.Namespace != "default" {
		t.Errorf("web namespace = %s, want default", web.Namespace)
	}
	if web.CPURequestMilli != 500 {
		t.Errorf("web CPU milli = %d, want 500", web.CPURequestMilli)
	}
	if web.CPURequestVCPU != 0.5 {
		t.Errorf("web CPU vCPU = %f, want 0.5", web.CPURequestVCPU)
	}
	if web.IsSpot {
		t.Error("web should not be spot")
	}
	if web.Labels["team"] != "platform" {
		t.Errorf("web team label = %s, want platform", web.Labels["team"])
	}

	// Verify worker pod (multi-container, spot)
	if worker.CPURequestMilli != 1250 {
		t.Errorf("worker CPU milli = %d, want 1250 (1000+250)", worker.CPURequestMilli)
	}
	expectedMemBytes := int64(1*1024*1024*1024 + 128*1024*1024)
	if worker.MemRequestBytes != expectedMemBytes {
		t.Errorf("worker mem bytes = %d, want %d", worker.MemRequestBytes, expectedMemBytes)
	}
	if !worker.IsSpot {
		t.Error("worker should be spot")
	}
}

func TestListPodsNamespaceFilter(t *testing.T) {
	startTime := metav1.NewTime(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC))

	pods := []runtime.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "c", Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
				}}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, StartTime: &startTime},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod2", Namespace: "ns2"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "c", Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
				}}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, StartTime: &startTime},
		},
	}

	client := fake.NewSimpleClientset(pods...)
	pl, err := NewPodLister(WithClient(client), WithNamespace("ns1"))
	if err != nil {
		t.Fatal(err)
	}

	result, err := pl.ListPods(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 pod in ns1, got %d", len(result))
	}
	if result[0].Name != "pod1" {
		t.Errorf("expected pod1, got %s", result[0].Name)
	}
}

func TestIsSpotPod(t *testing.T) {
	tests := []struct {
		name         string
		nodeSelector map[string]string
		expected     bool
	}{
		{"no node selector", nil, false},
		{"empty node selector", map[string]string{}, false},
		{"gke-spot true", map[string]string{"cloud.google.com/gke-spot": "true"}, true},
		{"compute-class autopilot-spot", map[string]string{"cloud.google.com/compute-class": "autopilot-spot"}, true},
		{"unrelated selector", map[string]string{"some-key": "some-value"}, false},
		{"gke-spot false", map[string]string{"cloud.google.com/gke-spot": "false"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{NodeSelector: tt.nodeSelector},
			}
			if got := isSpotPod(pod); got != tt.expected {
				t.Errorf("isSpotPod() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExtractPodInfoZeroRequests(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "c"}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	info := extractPodInfo(pod)
	if info.CPURequestMilli != 0 {
		t.Errorf("expected 0 CPU milli, got %d", info.CPURequestMilli)
	}
	if info.MemRequestBytes != 0 {
		t.Errorf("expected 0 mem bytes, got %d", info.MemRequestBytes)
	}
	if info.StartTime != (time.Time{}) {
		t.Errorf("expected zero start time, got %v", info.StartTime)
	}
}
