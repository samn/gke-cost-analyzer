package kube

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
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
				NodeName: "gk3-cluster-pool-abc123",
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
				NodeName: "gk3-cluster-spot-def456",
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
	if web.Phase != corev1.PodRunning {
		t.Errorf("web phase = %s, want Running", web.Phase)
	}
	if !web.StartTime.Equal(startTime.Time) {
		t.Errorf("web start time = %v, want %v", web.StartTime, startTime.Time)
	}
	// 256 Mi = 268435456 bytes → GB: 268435456 / 1e9 = 0.268435456
	expectedWebMemGB := float64(256*1024*1024) / 1e9
	if web.MemRequestGB != expectedWebMemGB {
		t.Errorf("web MemRequestGB = %f, want %f", web.MemRequestGB, expectedWebMemGB)
	}

	// Verify worker pod (multi-container, spot)
	if worker.CPURequestMilli != 1250 {
		t.Errorf("worker CPU milli = %d, want 1250 (1000+250)", worker.CPURequestMilli)
	}
	if worker.CPURequestVCPU != 1.25 {
		t.Errorf("worker CPU vCPU = %f, want 1.25", worker.CPURequestVCPU)
	}
	expectedMemBytes := int64(1*1024*1024*1024 + 128*1024*1024)
	if worker.MemRequestBytes != expectedMemBytes {
		t.Errorf("worker mem bytes = %d, want %d", worker.MemRequestBytes, expectedMemBytes)
	}
	expectedWorkerMemGB := float64(expectedMemBytes) / 1e9
	if worker.MemRequestGB != expectedWorkerMemGB {
		t.Errorf("worker MemRequestGB = %f, want %f", worker.MemRequestGB, expectedWorkerMemGB)
	}
	if !worker.IsSpot {
		t.Error("worker should be spot")
	}
	if worker.Phase != corev1.PodRunning {
		t.Errorf("worker phase = %s, want Running", worker.Phase)
	}
	if !worker.StartTime.Equal(startTime.Time) {
		t.Errorf("worker start time = %v, want %v", worker.StartTime, startTime.Time)
	}
}

func TestListPodsFieldSelectorRunning(t *testing.T) {
	// The fake clientset doesn't enforce FieldSelectors, so we verify
	// the selector is correctly passed by inspecting the recorded action.
	pods := []runtime.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "running", Namespace: "default"},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c"}}},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "succeeded", Namespace: "default"},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c"}}},
			Status:     corev1.PodStatus{Phase: corev1.PodSucceeded},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "failed", Namespace: "default"},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c"}}},
			Status:     corev1.PodStatus{Phase: corev1.PodFailed},
		},
	}

	client := fake.NewSimpleClientset(pods...)
	pl, err := NewPodLister(WithClient(client))
	if err != nil {
		t.Fatal(err)
	}

	_, err = pl.ListPods(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Verify the field selector for status.phase=Running was sent
	foundSelector := false
	for _, action := range client.Actions() {
		if listAction, ok := action.(ktesting.ListActionImpl); ok {
			fs := listAction.GetListRestrictions().Fields
			if fs.Matches(fields.Set{"status.phase": "Running"}) {
				foundSelector = true
				break
			}
		}
	}
	if !foundSelector {
		t.Error("expected ListPods to use field selector status.phase=Running")
	}
}

func TestListPodsNamespaceFilter(t *testing.T) {
	startTime := metav1.NewTime(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC))

	pods := []runtime.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"},
			Spec: corev1.PodSpec{
				NodeName: "gk3-cluster-pool-1",
				Containers: []corev1.Container{{Name: "c", Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
				}}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, StartTime: &startTime},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod2", Namespace: "ns2"},
			Spec: corev1.PodSpec{
				NodeName: "gk3-cluster-pool-2",
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

func TestListPodsAPIError(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("API server unavailable")
	})

	pl, err := NewPodLister(WithClient(client))
	if err != nil {
		t.Fatal(err)
	}

	_, err = pl.ListPods(context.Background())
	if err == nil {
		t.Fatal("expected error from API failure")
	}
	if err.Error() != "API server unavailable" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestListPodsFiltersNonAutopilotNodes(t *testing.T) {
	startTime := metav1.NewTime(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC))

	pods := []runtime.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "autopilot-pod", Namespace: "default"},
			Spec: corev1.PodSpec{
				NodeName:   "gk3-cluster-pool-abc123",
				Containers: []corev1.Container{{Name: "c", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}}}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, StartTime: &startTime},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "standard-pod", Namespace: "default"},
			Spec: corev1.PodSpec{
				NodeName:   "gke-cluster-pool-def456",
				Containers: []corev1.Container{{Name: "c", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}}}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, StartTime: &startTime},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "unscheduled-pod", Namespace: "default"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "c", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}}}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, StartTime: &startTime},
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

	if len(result) != 1 {
		t.Fatalf("expected 1 pod (autopilot only), got %d", len(result))
	}
	if result[0].Name != "autopilot-pod" {
		t.Errorf("expected autopilot-pod, got %s", result[0].Name)
	}
}

func TestIsAutopilotNode(t *testing.T) {
	tests := []struct {
		name     string
		nodeName string
		expected bool
	}{
		{"autopilot node", "gk3-cluster-pool-abc123", true},
		{"standard node", "gke-cluster-pool-abc123", false},
		{"empty node name", "", false},
		{"other prefix", "custom-node-1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAutopilotNode(tt.nodeName); got != tt.expected {
				t.Errorf("isAutopilotNode(%q) = %v, want %v", tt.nodeName, got, tt.expected)
			}
		})
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

func TestNewTestPodInfo(t *testing.T) {
	startTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	labels := map[string]string{"team": "platform", "app": "web"}
	pi := NewTestPodInfo("test-pod", "test-ns", 500, 1024, startTime, true, labels)

	if pi.Name != "test-pod" {
		t.Errorf("name = %s, want test-pod", pi.Name)
	}
	if pi.Namespace != "test-ns" {
		t.Errorf("namespace = %s, want test-ns", pi.Namespace)
	}
	if pi.CPURequestMilli != 500 {
		t.Errorf("CPU milli = %d, want 500", pi.CPURequestMilli)
	}
	if pi.CPURequestVCPU != 0.5 {
		t.Errorf("CPU vCPU = %f, want 0.5", pi.CPURequestVCPU)
	}
	// 1024 MB = 1024 * 1,000,000 bytes = 1,024,000,000 bytes
	expectedMemBytes := int64(1024 * 1_000_000)
	if pi.MemRequestBytes != expectedMemBytes {
		t.Errorf("mem bytes = %d, want %d", pi.MemRequestBytes, expectedMemBytes)
	}
	expectedMemGB := float64(expectedMemBytes) / 1e9
	if pi.MemRequestGB != expectedMemGB {
		t.Errorf("mem GB = %f, want %f", pi.MemRequestGB, expectedMemGB)
	}
	if !pi.StartTime.Equal(startTime) {
		t.Errorf("start time = %v, want %v", pi.StartTime, startTime)
	}
	if !pi.IsSpot {
		t.Error("expected IsSpot to be true")
	}
	if pi.Phase != corev1.PodRunning {
		t.Errorf("phase = %s, want Running", pi.Phase)
	}
	if pi.Labels["team"] != "platform" {
		t.Errorf("team label = %s, want platform", pi.Labels["team"])
	}
}

func TestNewTestPodInfoZeroValues(t *testing.T) {
	pi := NewTestPodInfo("empty", "ns", 0, 0, time.Time{}, false, nil)

	if pi.CPURequestVCPU != 0 {
		t.Errorf("expected 0 vCPU, got %f", pi.CPURequestVCPU)
	}
	if pi.MemRequestGB != 0 {
		t.Errorf("expected 0 GB, got %f", pi.MemRequestGB)
	}
	if pi.Labels != nil {
		t.Errorf("expected nil labels, got %v", pi.Labels)
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
	if info.Phase != corev1.PodRunning {
		t.Errorf("expected Running phase, got %s", info.Phase)
	}
}
