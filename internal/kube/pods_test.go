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

	t.Run("autopilot mode", func(t *testing.T) {
		client := fake.NewSimpleClientset(pods...)
		pl, err := NewPodLister(WithClient(client), WithClusterMode(ClusterModeAutopilot))
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
		if !result[0].IsAutopilot {
			t.Error("expected IsAutopilot = true")
		}
	})

	t.Run("standard mode", func(t *testing.T) {
		client := fake.NewSimpleClientset(pods...)
		pl, err := NewPodLister(WithClient(client), WithClusterMode(ClusterModeStandard))
		if err != nil {
			t.Fatal(err)
		}

		result, err := pl.ListPods(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		if len(result) != 1 {
			t.Fatalf("expected 1 pod (standard only), got %d", len(result))
		}
		if result[0].Name != "standard-pod" {
			t.Errorf("expected standard-pod, got %s", result[0].Name)
		}
		if result[0].IsAutopilot {
			t.Error("expected IsAutopilot = false")
		}
	})

	t.Run("all mode", func(t *testing.T) {
		client := fake.NewSimpleClientset(pods...)
		pl, err := NewPodLister(WithClient(client), WithClusterMode(ClusterModeAll))
		if err != nil {
			t.Fatal(err)
		}

		result, err := pl.ListPods(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		// Should include autopilot and standard, but not unscheduled
		if len(result) != 2 {
			t.Fatalf("expected 2 pods (autopilot + standard), got %d", len(result))
		}
		names := map[string]bool{}
		for _, p := range result {
			names[p.Name] = true
		}
		if !names["autopilot-pod"] {
			t.Error("expected autopilot-pod")
		}
		if !names["standard-pod"] {
			t.Error("expected standard-pod")
		}
	})

	t.Run("default mode is all", func(t *testing.T) {
		client := fake.NewSimpleClientset(pods...)
		pl, err := NewPodLister(WithClient(client))
		if err != nil {
			t.Fatal(err)
		}

		result, err := pl.ListPods(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		// Default mode is all — includes both autopilot and standard
		if len(result) != 2 {
			t.Fatalf("expected 2 pods (default=all), got %d", len(result))
		}
	})
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

func TestExtractPodInfoInitContainersNotCounted(t *testing.T) {
	// Init containers are NOT counted in Autopilot billing — only regular
	// containers contribute to resource requests.
	startTime := metav1.NewTime(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "with-init", Namespace: "default"},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Name: "init",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name: "main",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, StartTime: &startTime},
	}

	info := extractPodInfo(pod)
	// Only the main container's 500m should be counted, not the init container's 2000m
	if info.CPURequestMilli != 500 {
		t.Errorf("CPU milli = %d, want 500 (init container should not be counted)", info.CPURequestMilli)
	}
	// Only the main container's 256Mi should be counted
	expectedMemBytes := int64(256 * 1024 * 1024)
	if info.MemRequestBytes != expectedMemBytes {
		t.Errorf("mem bytes = %d, want %d (init container should not be counted)", info.MemRequestBytes, expectedMemBytes)
	}
}

func TestExtractPodInfoMemoryUnitConversion(t *testing.T) {
	// Verify the Kubernetes binary units (Mi, Gi) are correctly converted to
	// SI GB (10^9 bytes) for billing purposes.
	startTime := metav1.NewTime(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "mem-test", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "main",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, StartTime: &startTime},
	}

	info := extractPodInfo(pod)
	// 1 Gi = 1073741824 bytes
	if info.MemRequestBytes != 1073741824 {
		t.Errorf("1Gi should be 1073741824 bytes, got %d", info.MemRequestBytes)
	}
	// GB conversion: 1073741824 / 1e9 = 1.073741824 (NOT 1.0)
	expectedGB := 1073741824.0 / 1e9
	if info.MemRequestGB != expectedGB {
		t.Errorf("1Gi in GB = %f, want %f (binary vs SI)", info.MemRequestGB, expectedGB)
	}
}

func TestIsSpotPodBothSelectors(t *testing.T) {
	// Pod with both SPOT selectors set should still be detected as SPOT.
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"cloud.google.com/gke-spot":      "true",
				"cloud.google.com/compute-class": "autopilot-spot",
			},
		},
	}
	if !isSpotPod(pod) {
		t.Error("pod with both SPOT selectors should be detected as SPOT")
	}
}

func TestListPodsExcludeNamespaces(t *testing.T) {
	startTime := metav1.NewTime(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC))

	makePod := func(name, ns string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: corev1.PodSpec{
				NodeName: "gk3-cluster-pool-abc123",
				Containers: []corev1.Container{{
					Name: "c",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
					},
				}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, StartTime: &startTime},
		}
	}

	pods := []runtime.Object{
		makePod("app-pod", "default"),
		makePod("fluentbit", "kube-system"),
		makePod("gmp-collector", "gmp-system"),
		makePod("other-pod", "production"),
	}

	t.Run("default excludes kube-system and gmp-system", func(t *testing.T) {
		client := fake.NewSimpleClientset(pods...)
		pl, err := NewPodLister(
			WithClient(client),
			WithExcludeNamespaces([]string{"kube-system", "gmp-system"}),
		)
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
		names := map[string]bool{}
		for _, p := range result {
			names[p.Name] = true
		}
		if !names["app-pod"] {
			t.Error("expected app-pod to be included")
		}
		if !names["other-pod"] {
			t.Error("expected other-pod to be included")
		}
		if names["fluentbit"] {
			t.Error("expected fluentbit (kube-system) to be excluded")
		}
		if names["gmp-collector"] {
			t.Error("expected gmp-collector (gmp-system) to be excluded")
		}
	})

	t.Run("empty exclude list includes all namespaces", func(t *testing.T) {
		client := fake.NewSimpleClientset(pods...)
		pl, err := NewPodLister(
			WithClient(client),
			WithExcludeNamespaces([]string{}),
		)
		if err != nil {
			t.Fatal(err)
		}

		result, err := pl.ListPods(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		if len(result) != 4 {
			t.Fatalf("expected 4 pods with empty exclusion, got %d", len(result))
		}
	})

	t.Run("no exclude option includes all namespaces", func(t *testing.T) {
		client := fake.NewSimpleClientset(pods...)
		pl, err := NewPodLister(WithClient(client))
		if err != nil {
			t.Fatal(err)
		}

		result, err := pl.ListPods(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		if len(result) != 4 {
			t.Fatalf("expected 4 pods without exclusion option, got %d", len(result))
		}
	})

	t.Run("custom exclude list", func(t *testing.T) {
		client := fake.NewSimpleClientset(pods...)
		pl, err := NewPodLister(
			WithClient(client),
			WithExcludeNamespaces([]string{"kube-system", "gmp-system", "production"}),
		)
		if err != nil {
			t.Fatal(err)
		}

		result, err := pl.ListPods(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		if len(result) != 1 {
			t.Fatalf("expected 1 pod, got %d", len(result))
		}
		if result[0].Name != "app-pod" {
			t.Errorf("expected app-pod, got %s", result[0].Name)
		}
	})
}

func TestHasAutopilotLabels(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expected bool
	}{
		{"no labels", nil, false},
		{"empty labels", map[string]string{}, false},
		{"autopilot label", map[string]string{"autopilot.gke.io/something": "true"}, true},
		{"unrelated label", map[string]string{"app": "web"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasAutopilotLabels(tt.labels); got != tt.expected {
				t.Errorf("hasAutopilotLabels(%v) = %v, want %v", tt.labels, got, tt.expected)
			}
		})
	}
}

func TestHasNodePoolLabel(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expected bool
	}{
		{"no labels", nil, false},
		{"has nodepool label", map[string]string{"cloud.google.com/gke-nodepool": "pool-1"}, true},
		{"unrelated labels", map[string]string{"app": "web"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasNodePoolLabel(tt.labels); got != tt.expected {
				t.Errorf("hasNodePoolLabel(%v) = %v, want %v", tt.labels, got, tt.expected)
			}
		})
	}
}

func TestIncludeNodeLabelFallback(t *testing.T) {
	startTime := metav1.NewTime(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC))

	// Pod on a custom-named node with autopilot labels
	autopilotPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ap-pod", Namespace: "default",
			Labels: map[string]string{"autopilot.gke.io/resource-adjustment": "true"},
		},
		Spec: corev1.PodSpec{
			NodeName:   "custom-autopilot-node-1",
			Containers: []corev1.Container{{Name: "c", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}}}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, StartTime: &startTime},
	}

	// Pod on a custom-named node with nodepool label (standard)
	standardPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "std-pod", Namespace: "default",
			Labels: map[string]string{"cloud.google.com/gke-nodepool": "custom-pool"},
		},
		Spec: corev1.PodSpec{
			NodeName:   "custom-standard-node-1",
			Containers: []corev1.Container{{Name: "c", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}}}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, StartTime: &startTime},
	}

	pods := []runtime.Object{autopilotPod, standardPod}

	t.Run("all mode includes label-detected pods", func(t *testing.T) {
		client := fake.NewSimpleClientset(pods...)
		pl, err := NewPodLister(WithClient(client), WithClusterMode(ClusterModeAll))
		if err != nil {
			t.Fatal(err)
		}
		result, err := pl.ListPods(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 pods with label fallback, got %d", len(result))
		}
	})

	t.Run("autopilot mode detects via labels", func(t *testing.T) {
		client := fake.NewSimpleClientset(pods...)
		pl, err := NewPodLister(WithClient(client), WithClusterMode(ClusterModeAutopilot))
		if err != nil {
			t.Fatal(err)
		}
		result, err := pl.ListPods(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(result) != 1 || result[0].Name != "ap-pod" {
			t.Fatalf("expected only ap-pod, got %v", result)
		}
		if !result[0].IsAutopilot {
			t.Error("expected IsAutopilot = true for label-detected autopilot pod")
		}
	})

	t.Run("standard mode detects via labels", func(t *testing.T) {
		client := fake.NewSimpleClientset(pods...)
		pl, err := NewPodLister(WithClient(client), WithClusterMode(ClusterModeStandard))
		if err != nil {
			t.Fatal(err)
		}
		result, err := pl.ListPods(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(result) != 1 || result[0].Name != "std-pod" {
			t.Fatalf("expected only std-pod, got %v", result)
		}
	})
}

func TestExtractPodInfoPartialRequests(t *testing.T) {
	// Pod where only one container has CPU requests and another only has memory.
	startTime := metav1.NewTime(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "partial", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "cpu-only",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("500m"),
						},
					},
				},
				{
					Name: "mem-only",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, StartTime: &startTime},
	}

	info := extractPodInfo(pod)
	if info.CPURequestMilli != 500 {
		t.Errorf("CPU milli = %d, want 500", info.CPURequestMilli)
	}
	expectedMemBytes := int64(512 * 1024 * 1024)
	if info.MemRequestBytes != expectedMemBytes {
		t.Errorf("mem bytes = %d, want %d", info.MemRequestBytes, expectedMemBytes)
	}
}
