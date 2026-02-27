package cmd

import (
	"testing"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/kube"
)

func TestStripPodSuffix(t *testing.T) {
	tests := []struct {
		name string
		pod  string
		want string
	}{
		{
			name: "deployment pod with RS and pod hash",
			pod:  "coredns-7db6d8ff4d-x7p5r",
			want: "coredns",
		},
		{
			name: "deployment pod with longer name",
			pod:  "metrics-server-648b5df564-k2w9n",
			want: "metrics-server",
		},
		{
			name: "statefulset pod with ordinal",
			pod:  "redis-0",
			want: "redis",
		},
		{
			name: "statefulset pod with multi-digit ordinal",
			pod:  "redis-12",
			want: "redis",
		},
		{
			name: "statefulset with hyphenated name",
			pod:  "my-redis-cluster-3",
			want: "my-redis-cluster",
		},
		{
			name: "job pod with 5-char hash",
			pod:  "my-job-a1b2c",
			want: "my-job",
		},
		{
			name: "cronjob pod with schedule hash and pod hash",
			pod:  "my-cronjob-28405248-x7k9p",
			want: "my-cronjob",
		},
		{
			name: "plain name no suffix",
			pod:  "nginx",
			want: "nginx",
		},
		{
			name: "hyphenated name no random suffix",
			pod:  "nginx-proxy",
			want: "nginx-proxy",
		},
		{
			name: "does not strip all-alpha 5-char segment",
			pod:  "fluent-agent",
			want: "fluent-agent",
		},
		{
			name: "daemonset pod with hash",
			pod:  "fluent-bit-gke-2xf8q",
			want: "fluent-bit-gke",
		},
		{
			name: "preserves multi-segment base name",
			pod:  "my-cool-app-6b8f9c7d4f-ab1cd",
			want: "my-cool-app",
		},
		{
			name: "single segment name",
			pod:  "solo",
			want: "solo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripPodSuffix(tt.pod)
			if got != tt.want {
				t.Errorf("stripPodSuffix(%q) = %q, want %q", tt.pod, got, tt.want)
			}
		})
	}
}

func TestLooksLikeHash(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"x2k9p", true},
		{"7db6d8ff4d", true},
		{"abc123", true},
		{"proxy", false},   // no digits
		{"agent", false},   // no digits
		{"ABCDE", false},   // uppercase
		{"abc-de", false},  // contains hyphen
		{"", false},        // empty
		{"0", true},        // single digit
		{"a1", true},       // minimal hash
		{"12345", true},    // all digits
		{"abcde", false},   // all alpha, no digits
		{"ABC12", false},   // uppercase
		{"28405248", true}, // numeric but looks like hash
	}

	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			got := looksLikeHash(tt.s)
			if got != tt.want {
				t.Errorf("looksLikeHash(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"0", true},
		{"123", true},
		{"", false},
		{"abc", false},
		{"12a", false},
	}

	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			got := isNumeric(tt.s)
			if got != tt.want {
				t.Errorf("isNumeric(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestFindUnmatchedPods(t *testing.T) {
	now := time.Now()
	pods := []kube.PodInfo{
		kube.NewTestPodInfo("matched-pod-abc12-x1y2z", "ns1", 100, 256, now, false,
			map[string]string{"team": "platform", "app": "api"}),
		kube.NewTestPodInfo("no-team-pod-def34-a1b2c", "ns1", 100, 256, now, false,
			map[string]string{"app": "worker"}),
		kube.NewTestPodInfo("no-workload-pod-ghi56-d3e4f", "ns1", 100, 256, now, false,
			map[string]string{"team": "platform"}),
		kube.NewTestPodInfo("no-labels-pod-jkl78-g5h6i", "ns2", 100, 256, now, false,
			map[string]string{}),
		kube.NewTestPodInfo("nil-labels-pod-mno90-j7k8l", "ns2", 100, 256, now, false, nil),
	}

	t.Run("finds pods missing team or workload label", func(t *testing.T) {
		got := findUnmatchedPods(pods, "team", "app")
		if len(got) != 4 {
			t.Fatalf("got %d unmatched pods, want 4", len(got))
		}
		names := make([]string, len(got))
		for i, p := range got {
			names[i] = p.Name
		}
		wantNames := []string{
			"no-team-pod-def34-a1b2c",
			"no-workload-pod-ghi56-d3e4f",
			"no-labels-pod-jkl78-g5h6i",
			"nil-labels-pod-mno90-j7k8l",
		}
		for i, want := range wantNames {
			if names[i] != want {
				t.Errorf("unmatched[%d].Name = %q, want %q", i, names[i], want)
			}
		}
	})

	t.Run("all matched returns empty", func(t *testing.T) {
		matched := []kube.PodInfo{
			kube.NewTestPodInfo("pod-1", "ns1", 100, 256, now, false,
				map[string]string{"team": "a", "app": "b"}),
		}
		got := findUnmatchedPods(matched, "team", "app")
		if len(got) != 0 {
			t.Errorf("got %d unmatched, want 0", len(got))
		}
	})

	t.Run("empty team label skips team check", func(t *testing.T) {
		noTeamLabel := []kube.PodInfo{
			kube.NewTestPodInfo("pod-1", "ns1", 100, 256, now, false,
				map[string]string{"app": "worker"}),
		}
		got := findUnmatchedPods(noTeamLabel, "", "app")
		if len(got) != 0 {
			t.Errorf("got %d unmatched, want 0 (team label disabled)", len(got))
		}
	})

	t.Run("empty workload label skips workload check", func(t *testing.T) {
		noWorkloadLabel := []kube.PodInfo{
			kube.NewTestPodInfo("pod-1", "ns1", 100, 256, now, false,
				map[string]string{"team": "platform"}),
		}
		got := findUnmatchedPods(noWorkloadLabel, "team", "")
		if len(got) != 0 {
			t.Errorf("got %d unmatched, want 0 (workload label disabled)", len(got))
		}
	})

	t.Run("empty pods returns empty", func(t *testing.T) {
		got := findUnmatchedPods(nil, "team", "app")
		if len(got) != 0 {
			t.Errorf("got %d unmatched, want 0", len(got))
		}
	})
}

func TestGroupUnmatchedPods(t *testing.T) {
	now := time.Now()

	t.Run("groups by namespace and base name", func(t *testing.T) {
		pods := []kube.PodInfo{
			kube.NewTestPodInfo("api-7db6d8ff4d-x7p5r", "production", 100, 256, now, false, nil),
			kube.NewTestPodInfo("api-7db6d8ff4d-a1b2c", "production", 100, 256, now, false, nil),
			kube.NewTestPodInfo("worker-648b5df564-k2w9n", "production", 100, 256, now, false, nil),
			kube.NewTestPodInfo("api-9c8d7e6f5a-d3e4f", "staging", 100, 256, now, false, nil),
		}

		groups := groupUnmatchedPods(pods)

		if len(groups) != 3 {
			t.Fatalf("got %d groups, want 3", len(groups))
		}

		// Should be sorted: production/api, production/worker, staging/api
		wantGroups := []struct {
			ns       string
			baseName string
			count    int
		}{
			{"production", "api", 2},
			{"production", "worker", 1},
			{"staging", "api", 1},
		}

		for i, wg := range wantGroups {
			if groups[i].namespace != wg.ns {
				t.Errorf("group[%d].namespace = %q, want %q", i, groups[i].namespace, wg.ns)
			}
			if groups[i].baseName != wg.baseName {
				t.Errorf("group[%d].baseName = %q, want %q", i, groups[i].baseName, wg.baseName)
			}
			if len(groups[i].pods) != wg.count {
				t.Errorf("group[%d] has %d pods, want %d", i, len(groups[i].pods), wg.count)
			}
		}
	})

	t.Run("pod names within group are sorted", func(t *testing.T) {
		pods := []kube.PodInfo{
			kube.NewTestPodInfo("api-7db6d8ff4d-z9y8x", "ns", 100, 256, now, false, nil),
			kube.NewTestPodInfo("api-7db6d8ff4d-a1b2c", "ns", 100, 256, now, false, nil),
			kube.NewTestPodInfo("api-7db6d8ff4d-m5n6o", "ns", 100, 256, now, false, nil),
		}

		groups := groupUnmatchedPods(pods)
		if len(groups) != 1 {
			t.Fatalf("got %d groups, want 1", len(groups))
		}

		want := []string{
			"api-7db6d8ff4d-a1b2c",
			"api-7db6d8ff4d-m5n6o",
			"api-7db6d8ff4d-z9y8x",
		}
		for i, name := range groups[0].pods {
			if name != want[i] {
				t.Errorf("pods[%d] = %q, want %q", i, name, want[i])
			}
		}
	})

	t.Run("empty input returns empty", func(t *testing.T) {
		groups := groupUnmatchedPods(nil)
		if len(groups) != 0 {
			t.Errorf("got %d groups, want 0", len(groups))
		}
	})
}
