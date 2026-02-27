package envdefaults

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseGKEContext(t *testing.T) {
	tests := []struct {
		name        string
		context     string
		wantProject string
		wantRegion  string
		wantCluster string
		wantOK      bool
	}{
		{
			name:        "standard GKE context",
			context:     "gke_my-project_us-central1_my-cluster",
			wantProject: "my-project",
			wantRegion:  "us-central1",
			wantCluster: "my-cluster",
			wantOK:      true,
		},
		{
			name:        "zonal GKE context",
			context:     "gke_my-project_us-central1-a_my-cluster",
			wantProject: "my-project",
			wantRegion:  "us-central1",
			wantCluster: "my-cluster",
			wantOK:      true,
		},
		{
			name:        "complex project name",
			context:     "gke_org-project-123_europe-west1_prod-cluster",
			wantProject: "org-project-123",
			wantRegion:  "europe-west1",
			wantCluster: "prod-cluster",
			wantOK:      true,
		},
		{
			name:        "cluster name with underscores",
			context:     "gke_my-project_asia-east1_my_cluster_v2",
			wantProject: "my-project",
			wantRegion:  "asia-east1",
			wantCluster: "my_cluster_v2",
			wantOK:      true,
		},
		{
			name:   "non-GKE context",
			context: "minikube",
			wantOK: false,
		},
		{
			name:   "empty context",
			context: "",
			wantOK: false,
		},
		{
			name:   "gke prefix but too few parts",
			context: "gke_project_region",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, ok := parseGKEContext(tt.context)
			if ok != tt.wantOK {
				t.Fatalf("parseGKEContext(%q) ok = %v, want %v", tt.context, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if d.ProjectID != tt.wantProject {
				t.Errorf("project = %q, want %q", d.ProjectID, tt.wantProject)
			}
			if d.Region != tt.wantRegion {
				t.Errorf("region = %q, want %q", d.Region, tt.wantRegion)
			}
			if d.ClusterName != tt.wantCluster {
				t.Errorf("cluster = %q, want %q", d.ClusterName, tt.wantCluster)
			}
		})
	}
}

func TestZoneToRegion(t *testing.T) {
	tests := []struct {
		zone       string
		wantRegion string
	}{
		{"us-central1-a", "us-central1"},
		{"europe-west1-b", "europe-west1"},
		{"asia-east1-c", "asia-east1"},
		{"us-east4-a", "us-east4"},
		// Region passed through (no zone suffix)
		{"us-central1", "us-central1"},
	}

	for _, tt := range tests {
		t.Run(tt.zone, func(t *testing.T) {
			got := zoneToRegion(tt.zone)
			if got != tt.wantRegion {
				t.Errorf("zoneToRegion(%q) = %q, want %q", tt.zone, got, tt.wantRegion)
			}
		})
	}
}

func TestDetectorMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Metadata-Flavor") != "Google" {
			http.Error(w, "missing header", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/computeMetadata/v1/project/project-id":
			fmt.Fprint(w, "my-gke-project")
		case "/computeMetadata/v1/instance/attributes/cluster-name":
			fmt.Fprint(w, "my-gke-cluster")
		case "/computeMetadata/v1/instance/zone":
			fmt.Fprint(w, "projects/123456789/zones/us-central1-f")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d := NewDetector(
		WithMetadataBaseURL(srv.URL),
		WithKubeContext(""),
	)

	defaults := d.Detect()

	if defaults.ProjectID != "my-gke-project" {
		t.Errorf("project = %q, want my-gke-project", defaults.ProjectID)
	}
	if defaults.ClusterName != "my-gke-cluster" {
		t.Errorf("cluster = %q, want my-gke-cluster", defaults.ClusterName)
	}
	if defaults.Region != "us-central1" {
		t.Errorf("region = %q, want us-central1", defaults.Region)
	}
}

func TestDetectorMetadataPartialFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Metadata-Flavor") != "Google" {
			http.Error(w, "missing header", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/computeMetadata/v1/project/project-id":
			fmt.Fprint(w, "my-gke-project")
		case "/computeMetadata/v1/instance/attributes/cluster-name":
			http.Error(w, "not found", http.StatusNotFound)
		case "/computeMetadata/v1/instance/zone":
			http.Error(w, "not found", http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d := NewDetector(
		WithMetadataBaseURL(srv.URL),
		WithKubeContext(""),
	)

	defaults := d.Detect()

	// Project should still be set even though other fields failed
	if defaults.ProjectID != "my-gke-project" {
		t.Errorf("project = %q, want my-gke-project", defaults.ProjectID)
	}
	if defaults.ClusterName != "" {
		t.Errorf("cluster should be empty, got %q", defaults.ClusterName)
	}
	if defaults.Region != "" {
		t.Errorf("region should be empty, got %q", defaults.Region)
	}
}

func TestDetectorFallsBackToKubeContext(t *testing.T) {
	// Metadata server is unreachable
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not available", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	d := NewDetector(
		WithMetadataBaseURL(srv.URL),
		WithKubeContext("gke_dev-project_europe-west1_dev-cluster"),
	)

	defaults := d.Detect()

	if defaults.ProjectID != "dev-project" {
		t.Errorf("project = %q, want dev-project", defaults.ProjectID)
	}
	if defaults.ClusterName != "dev-cluster" {
		t.Errorf("cluster = %q, want dev-cluster", defaults.ClusterName)
	}
	if defaults.Region != "europe-west1" {
		t.Errorf("region = %q, want europe-west1", defaults.Region)
	}
}

func TestDetectorMetadataFillsGapsFromKubeContext(t *testing.T) {
	// Metadata server only returns project
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Metadata-Flavor") != "Google" {
			http.Error(w, "missing header", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/computeMetadata/v1/project/project-id":
			fmt.Fprint(w, "metadata-project")
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	d := NewDetector(
		WithMetadataBaseURL(srv.URL),
		WithKubeContext("gke_kube-project_us-west1_kube-cluster"),
	)

	defaults := d.Detect()

	// Project from metadata (takes priority)
	if defaults.ProjectID != "metadata-project" {
		t.Errorf("project = %q, want metadata-project", defaults.ProjectID)
	}
	// Cluster and region filled from kube context
	if defaults.ClusterName != "kube-cluster" {
		t.Errorf("cluster = %q, want kube-cluster", defaults.ClusterName)
	}
	if defaults.Region != "us-west1" {
		t.Errorf("region = %q, want us-west1", defaults.Region)
	}
}

func TestDetectorNoSources(t *testing.T) {
	// No metadata server, non-GKE context
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not available", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	d := NewDetector(
		WithMetadataBaseURL(srv.URL),
		WithKubeContext("minikube"),
	)

	defaults := d.Detect()

	if defaults.ProjectID != "" {
		t.Errorf("project should be empty, got %q", defaults.ProjectID)
	}
	if defaults.ClusterName != "" {
		t.Errorf("cluster should be empty, got %q", defaults.ClusterName)
	}
	if defaults.Region != "" {
		t.Errorf("region should be empty, got %q", defaults.Region)
	}
}

func TestDetectorMetadataUnreachable(t *testing.T) {
	// Point at a server that doesn't exist
	d := NewDetector(
		WithMetadataBaseURL("http://192.0.2.1:1"), // RFC 5737 TEST-NET, should fail fast
		WithHTTPClient(&http.Client{Timeout: 1}),  // tiny timeout
		WithKubeContext("gke_fallback-project_us-east1_fallback-cluster"),
	)

	defaults := d.Detect()

	if defaults.ProjectID != "fallback-project" {
		t.Errorf("project = %q, want fallback-project", defaults.ProjectID)
	}
	if defaults.ClusterName != "fallback-cluster" {
		t.Errorf("cluster = %q, want fallback-cluster", defaults.ClusterName)
	}
	if defaults.Region != "us-east1" {
		t.Errorf("region = %q, want us-east1", defaults.Region)
	}
}

func TestDefaultsString(t *testing.T) {
	tests := []struct {
		name     string
		defaults Defaults
		want     string
	}{
		{
			name:     "all fields",
			defaults: Defaults{ProjectID: "proj", Region: "us-central1", ClusterName: "cluster"},
			want:     "project=proj, region=us-central1, cluster=cluster",
		},
		{
			name:     "partial fields",
			defaults: Defaults{Region: "us-central1"},
			want:     "region=us-central1",
		},
		{
			name:     "no fields",
			defaults: Defaults{},
			want:     "(none detected)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.defaults.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseMetadataZone(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantZone string
	}{
		{"full path", "projects/123456789/zones/us-central1-f", "us-central1-f"},
		{"just zone", "us-central1-f", "us-central1-f"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMetadataZone(tt.raw)
			if got != tt.wantZone {
				t.Errorf("parseMetadataZone(%q) = %q, want %q", tt.raw, got, tt.wantZone)
			}
		})
	}
}
