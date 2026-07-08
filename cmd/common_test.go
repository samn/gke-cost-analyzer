package cmd

import "testing"

func TestParseTableRef(t *testing.T) {
	tests := []struct {
		name           string
		table          string
		defaultProject string
		defaultDataset string
		wantProject    string
		wantDataset    string
		wantTable      string
		wantErr        bool
	}{
		{
			name:           "bare table uses defaults",
			table:          "cost_snapshots",
			defaultProject: "detected-proj",
			defaultDataset: "gke_costs",
			wantProject:    "detected-proj",
			wantDataset:    "gke_costs",
			wantTable:      "cost_snapshots",
		},
		{
			name:           "dataset.table overrides dataset",
			table:          "other_ds.cost_snapshots",
			defaultProject: "detected-proj",
			defaultDataset: "gke_costs",
			wantProject:    "detected-proj",
			wantDataset:    "other_ds",
			wantTable:      "cost_snapshots",
		},
		{
			name:           "project.dataset.table overrides project and dataset",
			table:          "cost-central.gke_costs.cost_snapshots",
			defaultProject: "detected-proj",
			defaultDataset: "gke_costs",
			wantProject:    "cost-central",
			wantDataset:    "gke_costs",
			wantTable:      "cost_snapshots",
		},
		{
			name:           "bare table with empty default project keeps it empty",
			table:          "cost_snapshots",
			defaultProject: "",
			defaultDataset: "gke_costs",
			wantProject:    "",
			wantDataset:    "gke_costs",
			wantTable:      "cost_snapshots",
		},
		{
			name:    "too many parts is an error",
			table:   "a.b.c.d",
			wantErr: true,
		},
		{
			name:    "trailing dot yields empty part",
			table:   "gke_costs.",
			wantErr: true,
		},
		{
			name:    "leading dot yields empty part",
			table:   ".cost_snapshots",
			wantErr: true,
		},
		{
			name:    "empty string is an error",
			table:   "",
			wantErr: true,
		},
		{
			name:    "illegal character is an error",
			table:   "cost snapshots",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project, dataset, table, err := parseTableRef(tt.table, tt.defaultProject, tt.defaultDataset)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for table %q, got none", tt.table)
				}
				if !IsUsageError(err) {
					t.Errorf("error should be a usage error, got %T: %v", err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if project != tt.wantProject || dataset != tt.wantDataset || table != tt.wantTable {
				t.Errorf("parseTableRef(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.table, project, dataset, table, tt.wantProject, tt.wantDataset, tt.wantTable)
			}
		})
	}
}

func TestBigQueryProjectPrecedence(t *testing.T) {
	defer resetProjectState()()

	detectedProject = "detected-proj"
	if got := bigQueryProject(); got != "detected-proj" {
		t.Errorf("bigQueryProject() = %q, want detected-proj (fallback)", got)
	}

	bigqueryProjectID = "explicit-bq"
	if got := bigQueryProject(); got != "explicit-bq" {
		t.Errorf("bigQueryProject() = %q, want explicit-bq (flag wins)", got)
	}
}

func TestPrometheusProjectPrecedence(t *testing.T) {
	defer resetProjectState()()

	detectedProject = "detected-proj"
	if got := prometheusProject(); got != "detected-proj" {
		t.Errorf("prometheusProject() = %q, want detected-proj (fallback)", got)
	}

	prometheusProjectID = "explicit-prom"
	if got := prometheusProject(); got != "explicit-prom" {
		t.Errorf("prometheusProject() = %q, want explicit-prom (flag wins)", got)
	}
}
