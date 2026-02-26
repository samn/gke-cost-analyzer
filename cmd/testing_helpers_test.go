package cmd

import (
	"context"

	"github.com/samn/autopilot-cost-analyzer/internal/kube"
)

// mockPodLister implements the podLister interface for testing.
type mockPodLister struct {
	pods []kube.PodInfo
	err  error
}

func (m *mockPodLister) ListPods(_ context.Context) ([]kube.PodInfo, error) {
	return m.pods, m.err
}
