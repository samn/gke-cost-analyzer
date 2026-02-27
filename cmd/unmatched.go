package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"

	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(unmatchedCmd)
}

var unmatchedCmd = &cobra.Command{
	Use:   "unmatched-pods",
	Short: "List pods not matched to any team/workload",
	Long:  "Debug command to find running pods that are missing the configured team or workload labels.",
	RunE:  runUnmatched,
}

func runUnmatched(cmd *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer cancel()

	fmt.Println("Connecting to Kubernetes cluster...")
	lister, err := newPodLister()
	if err != nil {
		return fmt.Errorf("connecting to cluster: %w", err)
	}

	pods, err := lister.ListPods(ctx)
	if err != nil {
		return fmt.Errorf("listing pods: %w", err)
	}

	unmatched := findUnmatchedPods(pods, teamLabel, workloadLabel)
	printUnmatchedPods(unmatched, len(pods), teamLabel, workloadLabel)
	return nil
}

func findUnmatchedPods(pods []kube.PodInfo, teamLbl, workloadLbl string) []kube.PodInfo {
	var result []kube.PodInfo
	for _, p := range pods {
		missingTeam := teamLbl != "" && p.Labels[teamLbl] == ""
		missingWorkload := workloadLbl != "" && p.Labels[workloadLbl] == ""
		if missingTeam || missingWorkload {
			result = append(result, p)
		}
	}
	return result
}

// podGroup represents pods sharing the same base name and namespace.
type podGroup struct {
	namespace string
	baseName  string
	pods      []string
}

func groupUnmatchedPods(pods []kube.PodInfo) []podGroup {
	type groupKey struct {
		namespace string
		baseName  string
	}
	groups := make(map[groupKey]*podGroup)
	var order []groupKey

	for _, p := range pods {
		base := stripPodSuffix(p.Name)
		key := groupKey{namespace: p.Namespace, baseName: base}
		if _, ok := groups[key]; !ok {
			groups[key] = &podGroup{
				namespace: p.Namespace,
				baseName:  base,
			}
			order = append(order, key)
		}
		groups[key].pods = append(groups[key].pods, p.Name)
	}

	sort.Slice(order, func(i, j int) bool {
		if order[i].namespace != order[j].namespace {
			return order[i].namespace < order[j].namespace
		}
		return order[i].baseName < order[j].baseName
	})

	result := make([]podGroup, len(order))
	for i, key := range order {
		result[i] = *groups[key]
		sort.Strings(result[i].pods)
	}
	return result
}

// stripPodSuffix removes Kubernetes-generated random suffixes from pod names.
//
// Deployment pods: <name>-<rs-hash>-<pod-hash> (e.g. myapp-7b9f8c6d4f-x2k9p)
// StatefulSet pods: <name>-<ordinal> (e.g. redis-0)
// Job/DaemonSet pods: <name>-<hash> (e.g. my-job-a1b2c)
func stripPodSuffix(name string) string {
	parts := strings.Split(name, "-")
	n := len(parts)
	if n < 2 {
		return name
	}

	// Try stripping a 5-char hash suffix (pod hash).
	if len(parts[n-1]) == 5 && looksLikeHash(parts[n-1]) {
		n--
	}

	// Try stripping a ReplicaSet hash (6-10 chars with digits) or
	// StatefulSet ordinal (pure numeric).
	if n >= 2 {
		last := parts[n-1]
		isOrdinal := isNumeric(last)
		isRSHash := len(last) >= 6 && len(last) <= 10 && looksLikeHash(last)
		if isOrdinal || isRSHash {
			n--
		}
	}

	if n == 0 {
		return name
	}
	return strings.Join(parts[:n], "-")
}

// looksLikeHash returns true if s is lowercase alphanumeric and contains at
// least one digit, which distinguishes generated hashes from real name segments.
func looksLikeHash(s string) bool {
	if s == "" {
		return false
	}
	hasDigit := false
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			hasDigit = true
		case c >= 'a' && c <= 'z':
			// ok
		default:
			return false
		}
	}
	return hasDigit
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func printUnmatchedPods(unmatched []kube.PodInfo, totalPods int, teamLbl, workloadLbl string) {
	if len(unmatched) == 0 {
		fmt.Printf("All %d pods have team (%s) and workload (%s) labels set.\n",
			totalPods, teamLbl, workloadLbl)
		return
	}

	fmt.Printf("\nUnmatched pods: %d of %d total (missing label %q and/or %q)\n\n",
		len(unmatched), totalPods, teamLbl, workloadLbl)

	groups := groupUnmatchedPods(unmatched)

	currentNS := ""
	for _, g := range groups {
		if g.namespace != currentNS {
			if currentNS != "" {
				fmt.Println()
			}
			fmt.Printf("%s/\n", g.namespace)
			currentNS = g.namespace
		}
		noun := "pods"
		if len(g.pods) == 1 {
			noun = "pod"
		}
		fmt.Printf("  %s (%d %s)\n", g.baseName, len(g.pods), noun)
		for _, name := range g.pods {
			fmt.Printf("    %s\n", name)
		}
	}
}
