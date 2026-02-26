# autopilot-cost-analyzer

## Problem

We want to clearly see the cost for a workload (the aggregate cost of Pods grouped by specific labels) over time, and understand its efficiency.
While GKE (Google Kubernetes Engine) supports exporting billing & consumption data to BigQuery, it's difficult to query (especially for Autopilot workloads).

While cost attribution is quite complex for standard GKE workloads, we can take a simpler approach with Autopilot (on standard or Autopilot clusters).
Autopilot Pods are billed for their resource requests * running duration * resource cost.

## autopilot-cost-analyzer CLI

A tool (written in golang) to monitor usage of Autopilot workloads and either display a table of cost over time (`watch`) or write to bigquery.
There should be a command to create the necessary BigQuery dataset & tables. Prices should be fetched from the prices API and cached in `~/.cache`.
SPOT prices must be supported.

The data written to BigQuery must support the following types of queries:
- What is the total cost of a workload by a given label

Include the project id, region, and cluster name too.

The collected labels can be configured in advance to reduce cardinality.
We'll want to roll up metrics along several dimensions, e.g.
- team (top level)
- workload (i.e. Deployment/Argo Workflow name)
- subtype (optional, i.e. a specific step in an Argo Workflow)

The user should be able to configure the actual label names for that hiearchy.

The program should be efficient so that it can run collecting metrics for many Pods.


## Building
- Use golang
- Use mise to manage the environment
- IMPORTANT: everything must be tested.
- Use red/green TDD
- Ensure there are no warnings or errors
- Before committing make sure the lint/format/tests/compile all work
- Use prek for precommit hooks
- Make sure functionality is documented
