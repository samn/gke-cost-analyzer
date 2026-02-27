package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/envdefaults"
)

func TestApplyDefaultsFillsMissingRegion(t *testing.T) {
	saved := region
	defer func() { region = saved }()
	region = ""

	d := envdefaults.NewDetector(
		envdefaults.WithKubeContext("gke_my-project_us-central1_my-cluster"),
		envdefaults.WithHTTPClient(&http.Client{Timeout: 1}),
		envdefaults.WithMetadataBaseURL("http://192.0.2.1:1"),
	)

	applyDefaults(d, rootCmd)

	if region != "us-central1" {
		t.Errorf("region = %q, want us-central1", region)
	}
}

func TestApplyDefaultsFillsMissingProject(t *testing.T) {
	savedProject := project
	defer func() {
		project = savedProject
	}()
	project = ""

	d := envdefaults.NewDetector(
		envdefaults.WithKubeContext("gke_my-project_us-central1_my-cluster"),
		envdefaults.WithHTTPClient(&http.Client{Timeout: 1}),
		envdefaults.WithMetadataBaseURL("http://192.0.2.1:1"),
	)

	applyDefaults(d, rootCmd)

	if project != "my-project" {
		t.Errorf("project = %q, want my-project", project)
	}
}

func TestApplyDefaultsFillsMissingCluster(t *testing.T) {
	savedCluster := clusterName
	defer func() { clusterName = savedCluster }()
	clusterName = ""

	d := envdefaults.NewDetector(
		envdefaults.WithKubeContext("gke_my-project_us-central1_my-cluster"),
		envdefaults.WithHTTPClient(&http.Client{Timeout: 1}),
		envdefaults.WithMetadataBaseURL("http://192.0.2.1:1"),
	)

	applyDefaults(d, rootCmd)

	if clusterName != "my-cluster" {
		t.Errorf("clusterName = %q, want my-cluster", clusterName)
	}
}

func TestApplyDefaultsExplicitFlagWins(t *testing.T) {
	saved := region
	savedProject := project
	savedCluster := clusterName
	defer func() {
		region = saved
		project = savedProject
		clusterName = savedCluster
	}()

	// Simulate explicit values (already set by user)
	region = "europe-west1"
	project = "explicit-project"
	clusterName = "explicit-cluster"

	d := envdefaults.NewDetector(
		envdefaults.WithKubeContext("gke_inferred-project_us-central1_inferred-cluster"),
		envdefaults.WithHTTPClient(&http.Client{Timeout: 1}),
		envdefaults.WithMetadataBaseURL("http://192.0.2.1:1"),
	)

	applyDefaults(d, rootCmd)

	// Existing values should NOT be overwritten
	if region != "europe-west1" {
		t.Errorf("region = %q, want europe-west1 (explicit)", region)
	}
	if project != "explicit-project" {
		t.Errorf("project = %q, want explicit-project (explicit)", project)
	}
	if clusterName != "explicit-cluster" {
		t.Errorf("clusterName = %q, want explicit-cluster (explicit)", clusterName)
	}
}

func TestApplyDefaultsFromMetadataServer(t *testing.T) {
	saved := region
	savedProject := project
	savedCluster := clusterName
	defer func() {
		region = saved
		project = savedProject
		clusterName = savedCluster
	}()
	region = ""
	project = ""
	clusterName = ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Metadata-Flavor") != "Google" {
			http.Error(w, "missing header", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/computeMetadata/v1/project/project-id":
			fmt.Fprint(w, "gke-project")
		case "/computeMetadata/v1/instance/attributes/cluster-name":
			fmt.Fprint(w, "gke-cluster")
		case "/computeMetadata/v1/instance/zone":
			fmt.Fprint(w, "projects/123/zones/us-east4-b")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d := envdefaults.NewDetector(
		envdefaults.WithMetadataBaseURL(srv.URL),
		envdefaults.WithKubeContext(""),
	)

	applyDefaults(d, rootCmd)

	if region != "us-east4" {
		t.Errorf("region = %q, want us-east4", region)
	}
	if project != "gke-project" {
		t.Errorf("project = %q, want gke-project", project)
	}
	if clusterName != "gke-cluster" {
		t.Errorf("clusterName = %q, want gke-cluster", clusterName)
	}
}

func TestRecordPassesValidationWithInferredDefaults(t *testing.T) {
	saved := region
	savedProject := project
	savedCluster := clusterName
	savedInterval := recordInterval
	defer func() {
		region = saved
		project = savedProject
		clusterName = savedCluster
		recordInterval = savedInterval
	}()

	// Start with empty values
	region = ""
	project = ""
	clusterName = ""
	recordInterval = 5 * time.Minute

	// Apply defaults from kube context
	d := envdefaults.NewDetector(
		envdefaults.WithKubeContext("gke_inferred-project_us-central1_inferred-cluster"),
		envdefaults.WithHTTPClient(&http.Client{Timeout: 1}),
		envdefaults.WithMetadataBaseURL("http://192.0.2.1:1"),
	)
	applyDefaults(d, rootCmd)

	// Verify all values were filled
	if region != "us-central1" {
		t.Errorf("region = %q, want us-central1", region)
	}
	if project != "inferred-project" {
		t.Errorf("project = %q, want inferred-project", project)
	}
	if clusterName != "inferred-cluster" {
		t.Errorf("clusterName = %q, want inferred-cluster", clusterName)
	}
}
