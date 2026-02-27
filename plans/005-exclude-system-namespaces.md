# Plan: Exclude system namespaces by default

## Context

When running with `--namespace ""` (all namespaces, the default), the tool picks
up GKE system pods in `kube-system` and `gmp-system`. These are platform
overhead pods (DaemonSets like fluentbit, pdcsi-node, gke-metrics, GMP
collectors) that users don't deploy and can't label. They pollute cost
attribution and inflate the "unlabeled" bucket. We want to exclude them by
default.

## Approach

Add an `--exclude-namespaces` flag (string slice) with a default of
`kube-system,gmp-system`. Filtering is applied post-fetch in `ListPods()`,
alongside the existing `isAutopilotNode` check.

### Files modified

1. **`cmd/root.go`** — Add `excludeNamespaces []string` var and flag:
   ```
   --exclude-namespaces  Namespaces to exclude (default: kube-system,gmp-system)
   ```

2. **`cmd/common.go`** — Pass `excludeNamespaces` to `newPodLister()` via the
   new `WithExcludeNamespaces` option.

3. **`internal/kube/pods.go`** —
   - Add `excludeNamespaces map[string]bool` field to `PodLister`.
   - Add `WithExcludeNamespaces(ns []string) PodListerOption`.
   - In the `ListPods` loop (where we already skip non-Autopilot nodes), also
     skip pods whose namespace is in the exclude set. Use a `map[string]bool`
     built once for O(1) lookups.

4. **`internal/kube/pods_test.go`** — Add test `TestListPodsExcludeNamespaces`:
   create pods in `default`, `kube-system`, `gmp-system`, and `production`;
   verify correct filtering with default exclusions, empty exclusions, no
   exclusion option, and custom exclusion lists.

5. **`CHANGELOG.md`** — Add entry under `[Unreleased]`.

6. **`SPEC.md`** — Document `--exclude-namespaces` in Global flags and Pod
   Discovery sections.

### Behavior

- When `--namespace` is set to a specific namespace, `--exclude-namespaces` is
  effectively a no-op (Kubernetes API already filters to that one namespace).
- When `--namespace` is empty (all namespaces), excluded namespaces are filtered
  out post-fetch.
- Users can override: `--exclude-namespaces ""` to include everything, or
  `--exclude-namespaces "kube-system,gmp-system,istio-system"` to customize.

## Verification

```sh
go test ./internal/kube/ -run TestListPodsExcludeNamespaces -v
prek run --all-files
```
