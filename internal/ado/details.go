package ado

import (
	"cmp"
	"context"
	"strconv"
	"sync"
)

// Policy types the dashboard and the detail view tell apart.
const (
	PolicyTypeBuild  = "0609b952-1397-4640-95ec-e00a01b2c241"
	PolicyTypeStatus = "cbdc66da-9728-4af8-aada-9a5a32e4a226" // an external service's status check
)

type BuildState int

const (
	BuildNone BuildState = iota
	BuildQueued
	BuildRunning
	BuildPassed
	BuildPartial
	BuildFailed
)

// CICheckKind identifies where a PR check comes from.
type CICheckKind int

const (
	CICheckPolicyBuild CICheckKind = iota
	CICheckPRRun
	CICheckBranchRun
	CICheckExternal
)

// CICheck is one CI/CD pipeline run or external status check attached to a PR.
type CICheck struct {
	Kind         CICheckKind
	Name         string
	State        BuildState
	StatusText   string
	IsBlocking   bool
	Policy       *Policy
	Run          *Run
	ExternalURL  string
	DefinitionID int
}

// HasRun reports whether this check points to an Azure Pipelines run.
func (c CICheck) HasRun() bool { return c.Run != nil && c.Run.ID > 0 }

// IsCancellable reports whether this check's run can be cancelled.
func (c CICheck) IsCancellable() bool {
	if !c.HasRun() {
		return false
	}
	return c.Run.Status == "inProgress" || c.Run.Status == "notStarted" || c.Run.Status == "postponed"
}

// IsActive reports whether this check is currently executing or waiting in a
// run queue (excluding unstarted manual build policies with BuildID == 0).
func (c CICheck) IsActive() bool {
	if c.HasRun() && (c.Run.Status == "inProgress" || c.Run.Status == "cancelling" ||
		c.Run.Status == "notStarted" || c.Run.Status == "postponed") {
		return true
	}
	if c.Kind == CICheckPolicyBuild && c.Policy != nil && c.Policy.Status == "running" {
		return true
	}
	return c.Kind == CICheckExternal && c.State == BuildRunning
}

// CISummary aggregates every CI/CD check on a pull request.
type CISummary struct {
	State   BuildState
	Checks  []CICheck
	Passed  int
	Failed  int
	Running int
	Queued  int
	Partial int
}

// Total returns the number of checks included in the summary.
func (s CISummary) Total() int { return len(s.Checks) }

// HasActiveRun reports whether any check is currently running or queued with an
// active pipeline run.
func (s CISummary) HasActiveRun() bool {
	for _, c := range s.Checks {
		if c.IsActive() {
			return true
		}
	}
	return false
}

// SummarizeCI combines build policies, PR-triggered runs, source-branch runs,
// and external statuses into a deduplicated summary with precedence
// Failed > Running > Queued > Partial > Passed > None.
func SummarizeCI(policies []Policy, prRuns, branchRuns []Run, statuses []Status) CISummary {
	byID := make(map[int]Run, len(prRuns)+len(branchRuns))
	for _, r := range branchRuns {
		byID[r.ID] = r
	}
	for _, r := range prRuns {
		byID[r.ID] = r
	}

	var checks []CICheck
	seenBuild := map[string]bool{}
	coveredDef := map[int]bool{}
	seenRun := map[int]bool{}
	seenStatusKey := map[string]bool{}

	// 1. Enabled build validation policies.
	for _, p := range policies {
		if !p.Configuration.IsEnabled || p.Status == "notApplicable" {
			continue
		}
		if p.Configuration.Type.ID != PolicyTypeBuild {
			continue
		}
		defID := p.BuildDefinitionID()
		dedupKey := p.EvaluationID
		if defID != 0 {
			dedupKey = strconv.Itoa(defID)
		}
		if dedupKey != "" {
			if seenBuild[dedupKey] {
				continue
			}
			seenBuild[dedupKey] = true
		}

		var matchedRun *Run
		if bid := p.BuildID(); bid > 0 {
			if r, ok := byID[bid]; ok {
				rc := r
				matchedRun = &rc
			} else {
				matchedRun = &Run{ID: bid}
			}
			seenRun[bid] = true
		}
		if defID == 0 && matchedRun != nil {
			defID = matchedRun.Definition.ID
		}
		if defID != 0 {
			coveredDef[defID] = true
		}

		displayName, _ := p.Configuration.Settings["displayName"].(string)
		runName := ""
		if matchedRun != nil {
			runName = matchedRun.Definition.Name
		}
		name := cmp.Or(displayName, p.BuildName, runName, "Build")
		state, statusText := policyBuildState(p, matchedRun)
		pc := p
		checks = append(checks, CICheck{
			Kind:         CICheckPolicyBuild,
			Name:         name,
			State:        state,
			StatusText:   statusText,
			IsBlocking:   p.Configuration.IsBlocking,
			Policy:       &pc,
			Run:          matchedRun,
			DefinitionID: defID,
		})
	}

	// 2. PR-triggered runs (newest first).
	for _, r := range prRuns {
		if r.Definition.ID != 0 {
			if coveredDef[r.Definition.ID] {
				continue
			}
			coveredDef[r.Definition.ID] = true
		}
		if seenRun[r.ID] {
			continue
		}
		seenRun[r.ID] = true
		rc := r
		checks = append(checks, CICheck{
			Kind:         CICheckPRRun,
			Name:         cmp.Or(r.Definition.Name, r.BuildNumber, "Build"),
			State:        runBuildState(r),
			StatusText:   runStatusText(r),
			Run:          &rc,
			DefinitionID: r.Definition.ID,
		})
	}

	// 3. Source-branch runs (newest first).
	for _, r := range branchRuns {
		if r.Definition.ID != 0 {
			if coveredDef[r.Definition.ID] {
				continue
			}
			coveredDef[r.Definition.ID] = true
		}
		if seenRun[r.ID] {
			continue
		}
		seenRun[r.ID] = true
		rc := r
		checks = append(checks, CICheck{
			Kind:         CICheckBranchRun,
			Name:         cmp.Or(r.Definition.Name, r.BuildNumber, "Build"),
			State:        runBuildState(r),
			StatusText:   runStatusText(r),
			Run:          &rc,
			DefinitionID: r.Definition.ID,
		})
	}

	// 4a. Required / enabled external status policies.
	for _, p := range policies {
		if !p.Configuration.IsEnabled || p.Status == "notApplicable" {
			continue
		}
		if p.Configuration.Type.ID != PolicyTypeStatus {
			continue
		}
		key := p.StatusKey()
		if key != "" {
			if seenStatusKey[key] {
				continue
			}
			seenStatusKey[key] = true
		}
		s, ok := latestStatusForPolicy(p, statuses)
		defaultName, _ := p.Configuration.Settings["defaultDisplayName"].(string)
		desc := ""
		extURL := ""
		if ok {
			desc = s.Description
			extURL = s.TargetURL
		}
		name := cmp.Or(defaultName, desc, key, "External check")
		state, statusText := externalPolicyState(p, s, ok)
		pc := p
		checks = append(checks, CICheck{
			Kind:        CICheckExternal,
			Name:        name,
			State:       state,
			StatusText:  statusText,
			IsBlocking:  p.Configuration.IsBlocking,
			Policy:      &pc,
			ExternalURL: extURL,
		})
	}

	// 4b. Unrequired external statuses (newest per key).
	latestExt := map[string]Status{}
	var extOrder []string
	for _, s := range statuses {
		if s.State == "" || s.State == "notApplicable" || s.State == "notSet" {
			continue
		}
		k := s.Key()
		if seenStatusKey[k] {
			continue
		}
		old, seen := latestExt[k]
		if !seen {
			extOrder = append(extOrder, k)
		}
		if !seen || s.CreationDate.After(old.CreationDate) {
			latestExt[k] = s
		}
	}
	for _, k := range extOrder {
		s := latestExt[k]
		state, statusText := externalStatusState(s.State)
		checks = append(checks, CICheck{
			Kind:        CICheckExternal,
			Name:        cmp.Or(s.Description, k, "External check"),
			State:       state,
			StatusText:  statusText,
			ExternalURL: s.TargetURL,
		})
	}

	summary := CISummary{Checks: checks}
	for _, c := range checks {
		switch c.State {
		case BuildPassed:
			summary.Passed++
		case BuildFailed:
			summary.Failed++
		case BuildRunning:
			summary.Running++
		case BuildQueued:
			summary.Queued++
		case BuildPartial:
			summary.Partial++
		case BuildNone:
		}
	}
	switch {
	case summary.Failed > 0:
		summary.State = BuildFailed
	case summary.Running > 0:
		summary.State = BuildRunning
	case summary.Queued > 0:
		summary.State = BuildQueued
	case summary.Partial > 0:
		summary.State = BuildPartial
	case summary.Passed > 0:
		summary.State = BuildPassed
	}
	return summary
}

func policyBuildState(p Policy, r *Run) (BuildState, string) {
	switch p.Status {
	case "rejected", "broken":
		return BuildFailed, "failed"
	case "running":
		if r != nil && (r.Status == "notStarted" || r.Status == "postponed") {
			return BuildQueued, "queued"
		}
		return BuildRunning, "in progress"
	case "queued":
		return BuildQueued, "queued"
	case "approved":
		if r != nil && (r.Result == "partiallySucceeded" || r.Result == "succeededWithIssues") {
			return BuildPartial, "partially succeeded"
		}
		return BuildPassed, "succeeded"
	default:
		return BuildNone, p.Status
	}
}

func runBuildState(r Run) BuildState {
	switch r.Status {
	case "inProgress", "cancelling":
		return BuildRunning
	case "notStarted", "postponed":
		return BuildQueued
	}
	switch r.Result {
	case "failed":
		return BuildFailed
	case "partiallySucceeded", "succeededWithIssues":
		return BuildPartial
	case "succeeded":
		return BuildPassed
	case "canceled":
		return BuildNone
	}
	return BuildNone
}

func runStatusText(r Run) string {
	switch r.Status {
	case "inProgress":
		return "in progress"
	case "cancelling":
		return "cancelling"
	case "notStarted", "postponed":
		return "queued"
	}
	switch r.Result {
	case "failed":
		return "failed"
	case "partiallySucceeded", "succeededWithIssues":
		return "partially succeeded"
	case "succeeded":
		return "succeeded"
	case "canceled":
		return "canceled"
	case "skipped":
		return "skipped"
	}
	return cmp.Or(r.Result, r.Status, "unknown")
}

func externalStatusState(state string) (BuildState, string) {
	switch state {
	case "failed", "error":
		return BuildFailed, "failed"
	case "pending":
		return BuildRunning, "running"
	case "succeeded":
		return BuildPassed, "succeeded"
	}
	return BuildNone, state
}

func externalPolicyState(p Policy, s Status, ok bool) (BuildState, string) {
	if ok {
		if st, txt := externalStatusState(s.State); st != BuildNone {
			return st, txt
		}
	}
	switch p.Status {
	case "rejected", "broken":
		return BuildFailed, "failed"
	case "running":
		return BuildRunning, "running"
	case "approved":
		return BuildPassed, "succeeded"
	}
	if ok {
		return BuildNone, cmp.Or(s.State, p.Status, "waiting for status")
	}
	return BuildNone, "waiting for status"
}

// PRCI fetches policy evaluations, PR runs, source-branch runs, and external
// statuses in parallel and returns a deduplicated CI/CD summary for the PR.
func (c *Client) PRCI(ctx context.Context, pr PullRequest) (CISummary, error) {
	var (
		wg         sync.WaitGroup
		policies   []Policy
		prRuns     []Run
		branchRuns []Run
		statuses   []Status
		polErr     error
		prRunErr   error
		brRunErr   error
		statErr    error
	)
	wg.Go(func() {
		policies, polErr = c.rawPolicies(ctx, pr)
	})
	wg.Go(func() {
		prRuns, prRunErr = c.PRRuns(ctx, pr)
	})
	hasBranch := pr.SourceRefName != "" && pr.Repository.Project.ID != ""
	if hasBranch {
		wg.Go(func() {
			branchRuns, brRunErr = c.BranchRuns(ctx, pr)
		})
	}
	wg.Go(func() {
		statuses, statErr = c.statuses(ctx, pr)
	})
	wg.Wait()

	attempted := 3
	if hasBranch {
		attempted = 4
	}
	var firstErr error
	errCount := 0
	for _, err := range []error{polErr, prRunErr, brRunErr, statErr} {
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			errCount++
		}
	}
	if errCount == attempted {
		return CISummary{}, firstErr
	}

	byID := make(map[int]Run, len(prRuns)+len(branchRuns))
	for _, r := range branchRuns {
		byID[r.ID] = r
	}
	for _, r := range prRuns {
		byID[r.ID] = r
	}
	names := map[int]string{}
	for i := range policies {
		p := &policies[i]
		if !p.Configuration.IsEnabled || p.Status == "notApplicable" || p.Configuration.Type.ID != PolicyTypeBuild {
			continue
		}
		displayName, _ := p.Configuration.Settings["displayName"].(string)
		if displayName != "" {
			continue
		}
		if r, ok := byID[p.BuildID()]; ok && r.Definition.Name != "" {
			continue
		}
		if defID := p.BuildDefinitionID(); defID > 0 {
			p.BuildName = c.buildDefinitionName(ctx, pr, defID, names)
		}
	}
	return SummarizeCI(policies, prRuns, branchRuns, statuses), nil
}

// Build summarizes the PR's CI/CD checks using PRCI.
func (c *Client) Build(ctx context.Context, pr PullRequest) (BuildState, error) {
	s, err := c.PRCI(ctx, pr)
	return s.State, err
}
