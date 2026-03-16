package bigquery

import "testing"

func TestTableSchema(t *testing.T) {
	schema := TableSchema()

	expectedFields := []string{
		"timestamp", "project_id", "region", "cluster_name", "namespace",
		"team", "workload", "subtype", "pod_count", "cpu_request_vcpu",
		"memory_request_gb", "cpu_cost", "memory_cost", "total_cost",
		"is_spot", "interval_seconds", "cost_mode",
		"cpu_utilization", "memory_utilization", "efficiency_score", "wasted_cost",
	}

	if len(schema) != len(expectedFields) {
		t.Fatalf("expected %d fields, got %d", len(expectedFields), len(schema))
	}

	fieldNames := make(map[string]bool)
	for _, f := range schema {
		fieldNames[f.Name] = true
	}

	for _, name := range expectedFields {
		if !fieldNames[name] {
			t.Errorf("missing field: %s", name)
		}
	}
}

func TestTableSchemaTypes(t *testing.T) {
	schema := TableSchema()
	typeMap := make(map[string]string)
	for _, f := range schema {
		typeMap[f.Name] = f.Type
	}

	expectations := map[string]string{
		"timestamp":        "TIMESTAMP",
		"project_id":       "STRING",
		"pod_count":        "INT64",
		"cpu_request_vcpu": "FLOAT64",
		"is_spot":          "BOOL",
		"interval_seconds": "INT64",
	}

	for field, expectedType := range expectations {
		if typeMap[field] != expectedType {
			t.Errorf("field %s type = %s, want %s", field, typeMap[field], expectedType)
		}
	}
}

func TestTimePartitioning(t *testing.T) {
	tp := TimePartitioning()
	if tp["type"] != "DAY" {
		t.Errorf("partitioning type = %s, want DAY", tp["type"])
	}
	if tp["field"] != "timestamp" {
		t.Errorf("partitioning field = %s, want timestamp", tp["field"])
	}
}

func TestClustering(t *testing.T) {
	fields := Clustering()
	if len(fields) != 2 {
		t.Fatalf("expected 2 clustering fields, got %d", len(fields))
	}
	if fields[0] != "team" || fields[1] != "workload" {
		t.Errorf("clustering = %v, want [team, workload]", fields)
	}
}
