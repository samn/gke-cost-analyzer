package cmd

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestExecute(t *testing.T) {
	// Verify the root command can be executed without error
	// when called with no subcommands (prints help)
	rootCmd.SetArgs([]string{})
	if err := Execute(); err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
}

func TestLabelConfig(t *testing.T) {
	saved := teamLabel
	savedWL := workloadLabel
	savedST := subtypeLabel
	defer func() {
		teamLabel = saved
		workloadLabel = savedWL
		subtypeLabel = savedST
	}()

	teamLabel = "my-team"
	workloadLabel = "my-app"
	subtypeLabel = "my-sub"

	lc := labelConfig()
	if lc.TeamLabel != "my-team" {
		t.Errorf("team label = %s, want my-team", lc.TeamLabel)
	}
	if lc.WorkloadLabel != "my-app" {
		t.Errorf("workload label = %s, want my-app", lc.WorkloadLabel)
	}
	if lc.SubtypeLabel != "my-sub" {
		t.Errorf("subtype label = %s, want my-sub", lc.SubtypeLabel)
	}
}

func TestLabelConfigDefaults(t *testing.T) {
	saved := teamLabel
	savedWL := workloadLabel
	savedST := subtypeLabel
	defer func() {
		teamLabel = saved
		workloadLabel = savedWL
		subtypeLabel = savedST
	}()

	// Simulate defaults from root.go init()
	teamLabel = "team"
	workloadLabel = "app"
	subtypeLabel = ""

	lc := labelConfig()
	if lc.TeamLabel != "team" {
		t.Errorf("team label = %s, want team", lc.TeamLabel)
	}
	if lc.WorkloadLabel != "app" {
		t.Errorf("workload label = %s, want app", lc.WorkloadLabel)
	}
	if lc.SubtypeLabel != "" {
		t.Errorf("subtype label = %s, want empty", lc.SubtypeLabel)
	}
}

func TestValidateMode(t *testing.T) {
	saved := mode
	defer func() { mode = saved }()

	for _, valid := range []string{"autopilot", "standard", "all"} {
		mode = valid
		if err := validateMode(); err != nil {
			t.Errorf("validateMode() should accept %q, got: %v", valid, err)
		}
	}

	for _, invalid := range []string{"typo", "Auto", "ALL", ""} {
		mode = invalid
		if err := validateMode(); err == nil {
			t.Errorf("validateMode() should reject %q", invalid)
		}
	}
}

func TestNewPromClientExplicitURL(t *testing.T) {
	savedURL := prometheusURL
	savedProject := project
	defer func() {
		prometheusURL = savedURL
		project = savedProject
	}()

	prometheusURL = "http://my-prom:9090"
	project = ""

	client, err := newPromClient(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client when URL is set")
	}
}

func TestNewPromClientNoProjectNoURL(t *testing.T) {
	savedURL := prometheusURL
	savedProject := project
	defer func() {
		prometheusURL = savedURL
		project = savedProject
	}()

	prometheusURL = ""
	project = ""

	client, err := newPromClient(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client != nil {
		t.Error("expected nil client when no project and no URL")
	}
}

func TestNewPromClientGMPDefault(t *testing.T) {
	savedURL := prometheusURL
	savedProject := project
	savedFn := gcpHTTPClientFn
	defer func() {
		prometheusURL = savedURL
		project = savedProject
		gcpHTTPClientFn = savedFn
	}()

	prometheusURL = ""
	project = "my-gcp-project"

	// Mock GCP HTTP client to avoid needing real credentials
	gcpHTTPClientFn = func(_ context.Context, _ ...string) (*http.Client, error) {
		return &http.Client{}, nil
	}

	client, err := newPromClient(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client when project is available (GMP default)")
	}
}

func TestNewPromClientCustomURLTakesPriority(t *testing.T) {
	savedURL := prometheusURL
	savedProject := project
	savedFn := gcpHTTPClientFn
	defer func() {
		prometheusURL = savedURL
		project = savedProject
		gcpHTTPClientFn = savedFn
	}()

	prometheusURL = "http://custom-prom:9090"
	project = "my-gcp-project"

	// GCP client should NOT be called when custom URL is set
	gcpHTTPClientFn = func(_ context.Context, _ ...string) (*http.Client, error) {
		t.Fatal("gcpHTTPClientFn should not be called when custom URL is set")
		return nil, nil
	}

	client, err := newPromClient(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client when custom URL is set")
	}
}

func TestValidationErrorsAreUsageErrors(t *testing.T) {
	// Operator input mistakes must be classifiable so main() doesn't report
	// them to Sentry as application errors.
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
	region = ""
	project = "proj"
	clusterName = "cluster"
	recordInterval = 5 * time.Minute

	err := runRecord(rootCmd, nil)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !IsUsageError(err) {
		t.Errorf("missing --region should be a usage error, got %T: %v", err, err)
	}

	// Invalid --mode is also a usage error.
	savedMode := mode
	defer func() { mode = savedMode }()
	mode = "bogus"
	if err := validateMode(); err == nil || !IsUsageError(err) {
		t.Errorf("invalid mode should be a usage error, got %v", err)
	}

	// A non-usage error stays unclassified.
	if IsUsageError(context.DeadlineExceeded) {
		t.Error("arbitrary errors must not classify as usage errors")
	}
}
