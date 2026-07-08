package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samn/gke-cost-analyzer/internal/envdefaults"
)

// resetProjectState clears the project-related package vars and returns a
// function (for defer) that restores their previous values. It keeps detection
// tests hermetic and prevents cross-test contamination.
func resetProjectState() func() {
	savedBQ := bigqueryProjectID
	savedProm := prometheusProjectID
	savedURL := prometheusURL
	savedDetected := detectedProject
	bigqueryProjectID = ""
	prometheusProjectID = ""
	prometheusURL = ""
	detectedProject = ""
	return func() {
		bigqueryProjectID = savedBQ
		prometheusProjectID = savedProm
		prometheusURL = savedURL
		detectedProject = savedDetected
	}
}

func TestApplyDefaultsFillsMissingRegion(t *testing.T) {
	saved := region
	defer func() { region = saved }()
	defer resetProjectState()()
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

func TestApplyDefaultsRecordsDetectedProject(t *testing.T) {
	defer resetProjectState()()

	d := envdefaults.NewDetector(
		envdefaults.WithKubeContext("gke_my-project_us-central1_my-cluster"),
		envdefaults.WithHTTPClient(&http.Client{Timeout: 1}),
		envdefaults.WithMetadataBaseURL("http://192.0.2.1:1"),
	)

	applyDefaults(d, rootCmd)

	if detectedProject != "my-project" {
		t.Errorf("detectedProject = %q, want my-project", detectedProject)
	}
}

func TestApplyDefaultsFillsMissingCluster(t *testing.T) {
	savedCluster := clusterName
	defer func() { clusterName = savedCluster }()
	defer resetProjectState()()
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
	savedCluster := clusterName
	defer func() {
		region = saved
		clusterName = savedCluster
	}()
	defer resetProjectState()()

	// Simulate explicit values (already set by user). The project-source flags
	// stay empty, so detection still runs to recover the environment project.
	region = "europe-west1"
	clusterName = "explicit-cluster"

	d := envdefaults.NewDetector(
		envdefaults.WithKubeContext("gke_inferred-project_us-central1_inferred-cluster"),
		envdefaults.WithHTTPClient(&http.Client{Timeout: 1}),
		envdefaults.WithMetadataBaseURL("http://192.0.2.1:1"),
	)

	applyDefaults(d, rootCmd)

	// Explicit region/cluster must NOT be overwritten.
	if region != "europe-west1" {
		t.Errorf("region = %q, want europe-west1 (explicit)", region)
	}
	if clusterName != "explicit-cluster" {
		t.Errorf("clusterName = %q, want explicit-cluster (explicit)", clusterName)
	}
	// The detected project is still recorded for BigQuery/Prometheus defaults.
	if detectedProject != "inferred-project" {
		t.Errorf("detectedProject = %q, want inferred-project", detectedProject)
	}
}

func TestApplyDefaultsFromMetadataServer(t *testing.T) {
	saved := region
	savedCluster := clusterName
	defer func() {
		region = saved
		clusterName = savedCluster
	}()
	defer resetProjectState()()
	region = ""
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
	if detectedProject != "gke-project" {
		t.Errorf("detectedProject = %q, want gke-project", detectedProject)
	}
	if clusterName != "gke-cluster" {
		t.Errorf("clusterName = %q, want gke-cluster", clusterName)
	}
}

func TestRecordPassesValidationWithInferredDefaults(t *testing.T) {
	saved := region
	savedCluster := clusterName
	savedInterval := recordInterval
	defer func() {
		region = saved
		clusterName = savedCluster
		recordInterval = savedInterval
	}()
	defer resetProjectState()()

	// Start with empty values
	region = ""
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
	if detectedProject != "inferred-project" {
		t.Errorf("detectedProject = %q, want inferred-project", detectedProject)
	}
	if clusterName != "inferred-cluster" {
		t.Errorf("clusterName = %q, want inferred-cluster", clusterName)
	}
}

func TestApplyDefaultsDetectsProjectEvenWhenFlagsSet(t *testing.T) {
	saved := region
	savedCluster := clusterName
	defer func() {
		region = saved
		clusterName = savedCluster
	}()
	defer resetProjectState()()

	// Even when region, cluster, and both project-source flags are set — the
	// fully-specified record invocation — detection must still run so record can
	// attribute project_id to the actual cluster project (not the BigQuery
	// destination). This guards against silently misattributing every cluster's
	// costs to the central dataset's project.
	region = "europe-west1"
	clusterName = "explicit-cluster"
	bigqueryProjectID = "central-bq"
	prometheusProjectID = "prom-project"

	d := envdefaults.NewDetector(
		envdefaults.WithKubeContext("gke_cluster-project_us-central1_some-cluster"),
		envdefaults.WithHTTPClient(&http.Client{Timeout: 1}),
		envdefaults.WithMetadataBaseURL("http://192.0.2.1:1"),
	)

	applyDefaults(d, rootCmd)

	if detectedProject != "cluster-project" {
		t.Errorf("detectedProject = %q, want cluster-project (detection must run even with all flags set)", detectedProject)
	}
}

func TestApplyDefaultsRunsDetectionWhenProjectSourcesMissing(t *testing.T) {
	saved := region
	savedCluster := clusterName
	defer func() {
		region = saved
		clusterName = savedCluster
	}()
	defer resetProjectState()()

	// Region and cluster are set, but no project sources — Prometheus/BigQuery
	// still need an inferred project, so detection must run.
	region = "europe-west1"
	clusterName = "explicit-cluster"

	// Detection fans out concurrent metadata requests, so count them atomically.
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	d := envdefaults.NewDetector(
		envdefaults.WithMetadataBaseURL(srv.URL),
		envdefaults.WithKubeContext(""),
	)

	applyDefaults(d, rootCmd)

	if hits.Load() == 0 {
		t.Error("detection should have run to infer the project for BigQuery/Prometheus")
	}
}

func TestVersionCommandSkipsDetection(t *testing.T) {
	savedDetector := newDetector
	defer func() { newDetector = savedDetector }()

	called := false
	newDetector = func() *envdefaults.Detector {
		called = true
		return envdefaults.NewDetector(
			envdefaults.WithMetadataBaseURL("http://192.0.2.1:1"),
			envdefaults.WithKubeContext(""),
		)
	}

	rootCmd.SetArgs([]string{"version"})
	defer rootCmd.SetArgs(nil)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("version failed: %v", err)
	}

	if called {
		t.Error("version must not run environment detection (it needs no cluster/project)")
	}
}
