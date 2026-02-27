// Package envdefaults infers CLI defaults from the environment.
//
// It tries two strategies in order:
//  1. GCE metadata server (available inside GKE pods)
//  2. Kubeconfig current context parsing (for development)
//
// Explicit CLI flags always take priority over inferred defaults.
package envdefaults

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"k8s.io/client-go/tools/clientcmd"
)

const metadataBaseURL = "http://metadata.google.internal"

// Defaults holds values inferred from the environment.
type Defaults struct {
	ProjectID   string
	ClusterName string
	Region      string
}

// Detector detects CLI defaults from the environment.
type Detector struct {
	httpClient      *http.Client
	metadataBaseURL string
	kubeContext     string   // override for testing; empty means auto-detect
	kubeContextSet bool     // true when kubeContext was explicitly provided
}

// Option configures a Detector.
type Option func(*Detector)

// WithHTTPClient sets a custom HTTP client (for testing).
func WithHTTPClient(c *http.Client) Option {
	return func(d *Detector) { d.httpClient = c }
}

// WithMetadataBaseURL overrides the GCE metadata server base URL (for testing).
func WithMetadataBaseURL(url string) Option {
	return func(d *Detector) { d.metadataBaseURL = url }
}

// WithKubeContext overrides the kubeconfig context name (for testing).
func WithKubeContext(ctx string) Option {
	return func(d *Detector) {
		d.kubeContext = ctx
		d.kubeContextSet = true
	}
}

// NewDetector creates a Detector with the given options.
func NewDetector(opts ...Option) *Detector {
	d := &Detector{
		httpClient: &http.Client{Timeout: 1 * time.Second},
		metadataBaseURL: metadataBaseURL,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Detect returns inferred defaults by trying the metadata server first,
// then falling back to the kubeconfig context for any missing values.
func (d *Detector) Detect() Defaults {
	var defaults Defaults

	// Strategy 1: GCE metadata server
	defaults.ProjectID = d.fetchMetadata("/computeMetadata/v1/project/project-id")
	defaults.ClusterName = d.fetchMetadata("/computeMetadata/v1/instance/attributes/cluster-name")
	if raw := d.fetchMetadata("/computeMetadata/v1/instance/zone"); raw != "" {
		zone := parseMetadataZone(raw)
		defaults.Region = zoneToRegion(zone)
	}

	// Strategy 2: fill gaps from kubeconfig context
	ctxName := d.currentContext()
	if parsed, ok := parseGKEContext(ctxName); ok {
		if defaults.ProjectID == "" {
			defaults.ProjectID = parsed.ProjectID
		}
		if defaults.ClusterName == "" {
			defaults.ClusterName = parsed.ClusterName
		}
		if defaults.Region == "" {
			defaults.Region = parsed.Region
		}
	}

	return defaults
}

func (d *Detector) currentContext() string {
	if d.kubeContextSet {
		return d.kubeContext
	}
	return detectKubeContext()
}

func detectKubeContext() string {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	rawConfig, err := kubeConfig.RawConfig()
	if err != nil {
		return ""
	}
	return rawConfig.CurrentContext
}

func (d *Detector) fetchMetadata(path string) string {
	url := d.metadataBaseURL + path
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

// parseMetadataZone extracts the zone name from the metadata response.
// The metadata server returns a full resource path like "projects/123/zones/us-central1-f".
func parseMetadataZone(raw string) string {
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		return raw[i+1:]
	}
	return raw
}

// zoneToRegion converts a GCP zone (e.g. "us-central1-a") to a region (e.g. "us-central1").
// If the input is already a region (no zone suffix), it is returned unchanged.
var zoneRegex = regexp.MustCompile(`^(.+)-[a-z]$`)

func zoneToRegion(zone string) string {
	if m := zoneRegex.FindStringSubmatch(zone); m != nil {
		return m[1]
	}
	return zone
}

// parseGKEContext parses a GKE kubeconfig context name.
// GKE contexts follow the pattern: gke_PROJECT_LOCATION_CLUSTER
// where LOCATION is a region or zone and CLUSTER may contain underscores.
func parseGKEContext(ctx string) (Defaults, bool) {
	if !strings.HasPrefix(ctx, "gke_") {
		return Defaults{}, false
	}

	// Split into: "gke", PROJECT, LOCATION, CLUSTER...
	// The cluster name may contain underscores, so we split into at most 4 parts
	// after stripping the "gke_" prefix.
	rest := ctx[len("gke_"):]
	parts := strings.SplitN(rest, "_", 3)
	if len(parts) < 3 {
		return Defaults{}, false
	}

	project := parts[0]
	location := parts[1]
	cluster := parts[2]

	if project == "" || location == "" || cluster == "" {
		return Defaults{}, false
	}

	return Defaults{
		ProjectID:   project,
		Region:      zoneToRegion(location),
		ClusterName: cluster,
	}, true
}

// String returns a human-readable summary of the detected defaults.
func (d Defaults) String() string {
	var parts []string
	if d.ProjectID != "" {
		parts = append(parts, fmt.Sprintf("project=%s", d.ProjectID))
	}
	if d.Region != "" {
		parts = append(parts, fmt.Sprintf("region=%s", d.Region))
	}
	if d.ClusterName != "" {
		parts = append(parts, fmt.Sprintf("cluster=%s", d.ClusterName))
	}
	if len(parts) == 0 {
		return "(none detected)"
	}
	return strings.Join(parts, ", ")
}
