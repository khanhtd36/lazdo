package ado

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDeletedChangeKeepsItsPath(t *testing.T) {
	// A deletion as /iterations/{id}/changes returns it.
	raw := `{"changeTrackingId":101,"originalPath":"/web/src/hooks/useHistoryPath.ts",
		"item":{"originalObjectId":"5E71AA4F","path":null},"changeType":"delete"}`
	var ch Change
	if err := json.Unmarshal([]byte(raw), &ch); err != nil {
		t.Fatal(err)
	}
	if got := ch.withPath().Item.Path; got != "/web/src/hooks/useHistoryPath.ts" {
		t.Fatalf("a deleted file should keep its path, got %q", got)
	}
}

func makeBuildPolicy(evalID, status, name string, defID, buildID int, enabled, blocking bool) Policy {
	var p Policy
	p.EvaluationID = evalID
	p.Status = status
	p.BuildName = name
	p.Configuration.IsEnabled = enabled
	p.Configuration.IsBlocking = blocking
	p.Configuration.Type.ID = PolicyTypeBuild
	p.Configuration.Settings = map[string]any{
		"buildDefinitionId": float64(defID),
		"displayName":       name,
	}
	if buildID > 0 {
		p.Context = map[string]any{"buildId": float64(buildID)}
	}
	return p
}

func makeRun(id, defID int, defName, status, result string) Run {
	var r Run
	r.ID = id
	r.BuildNumber = "2026." + string(rune('0'+id%10))
	r.Status = status
	r.Result = result
	r.Definition.ID = defID
	r.Definition.Name = defName
	return r
}

func TestSummarizeCIDeduplicatesAndOrders(t *testing.T) {
	policies := []Policy{
		makeBuildPolicy("e1", "approved", "ci-build", 10, 101, true, true),
		makeBuildPolicy("e1-repo", "approved", "ci-build", 10, 101, true, true), // duplicate definition on repo
		makeBuildPolicy("e-disabled", "rejected", "off", 99, 199, false, true), // disabled
		makeBuildPolicy("e-na", "notApplicable", "na", 98, 198, true, true),    // notApplicable
	}
	prRuns := []Run{
		makeRun(101, 10, "ci-build", "completed", "succeeded"),          // covered by policy
		makeRun(102, 20, "pr-lint", "inProgress", ""),                   // PR-triggered non-policy
		makeRun(100, 20, "pr-lint", "completed", "failed"),              // older run of same PR pipeline
	}
	branchRuns := []Run{
		makeRun(103, 20, "pr-lint", "completed", "succeeded"),           // covered by PR run
		makeRun(104, 30, "branch-deploy", "notStarted", ""),             // source-branch run
		makeRun(105, 30, "branch-deploy", "completed", "failed"),        // older source-branch run
	}
	now := time.Now()
	var reqPol Policy
	reqPol.EvaluationID = "e-ext"
	reqPol.Status = "approved"
	reqPol.Configuration.IsEnabled = true
	reqPol.Configuration.IsBlocking = true
	reqPol.Configuration.Type.ID = PolicyTypeStatus
	reqPol.Configuration.Settings = map[string]any{
		"statusGenre":        "ci",
		"statusName":         "lint",
		"defaultDisplayName": "External Lint",
	}
	policies = append(policies, reqPol)

	statuses := []Status{
		{ID: 1, State: "succeeded", TargetURL: "https://ext/1", CreationDate: now.Add(-time.Minute), Context: struct {
			Name  string `json:"name"`
			Genre string `json:"genre"`
		}{Name: "lint", Genre: "ci"}},
		{ID: 2, State: "pending", Description: "Sonar", TargetURL: "https://sonar", CreationDate: now.Add(-2 * time.Minute), Context: struct {
			Name  string `json:"name"`
			Genre string `json:"genre"`
		}{Name: "sonar", Genre: "qa"}},
		{ID: 3, State: "succeeded", Description: "Sonar", TargetURL: "https://sonar/3", CreationDate: now, Context: struct {
			Name  string `json:"name"`
			Genre string `json:"genre"`
		}{Name: "sonar", Genre: "qa"}},
		{ID: 4, State: "notApplicable", Description: "Ignored", CreationDate: now, Context: struct {
			Name  string `json:"name"`
			Genre string `json:"genre"`
		}{Name: "ignored", Genre: "qa"}},
	}

	s := SummarizeCI(policies, prRuns, branchRuns, statuses)
	if got := s.Total(); got != 5 {
		t.Fatalf("expected 5 deduplicated checks (ci-build, pr-lint, branch-deploy, External Lint, Sonar), got %d: %+v", got, s.Checks)
	}
	if s.Checks[0].Kind != CICheckPolicyBuild || s.Checks[0].Name != "ci-build" || s.Checks[0].State != BuildPassed {
		t.Errorf("check[0] mismatch: %+v", s.Checks[0])
	}
	if s.Checks[1].Kind != CICheckPRRun || s.Checks[1].Name != "pr-lint" || s.Checks[1].State != BuildRunning {
		t.Errorf("check[1] mismatch: %+v", s.Checks[1])
	}
	if s.Checks[2].Kind != CICheckBranchRun || s.Checks[2].Name != "branch-deploy" || s.Checks[2].State != BuildQueued {
		t.Errorf("check[2] mismatch: %+v", s.Checks[2])
	}
	if s.Checks[3].Kind != CICheckExternal || s.Checks[3].Name != "External Lint" || s.Checks[3].State != BuildPassed || s.Checks[3].ExternalURL != "https://ext/1" {
		t.Errorf("check[3] mismatch: %+v", s.Checks[3])
	}
	if s.Checks[4].Kind != CICheckExternal || s.Checks[4].Name != "Sonar" || s.Checks[4].State != BuildPassed || s.Checks[4].ExternalURL != "https://sonar/3" {
		t.Errorf("check[4] mismatch: %+v", s.Checks[4])
	}
	if s.State != BuildRunning || s.Passed != 3 || s.Running != 1 || s.Queued != 1 {
		t.Errorf("summary counts/state wrong: %+v", s)
	}
	if !s.HasActiveRun() {
		t.Error("expected HasActiveRun() == true")
	}
}

func TestSummarizeCIPrecedence(t *testing.T) {
	cases := []struct {
		name   string
		checks []Run
		want   BuildState
	}{
		{
			name:   "failed beats running queued partial passed",
			checks: []Run{makeRun(1, 1, "a", "completed", "succeeded"), makeRun(2, 2, "b", "completed", "partiallySucceeded"), makeRun(3, 3, "c", "notStarted", ""), makeRun(4, 4, "d", "inProgress", ""), makeRun(5, 5, "e", "completed", "failed")},
			want:   BuildFailed,
		},
		{
			name:   "running beats queued partial passed",
			checks: []Run{makeRun(1, 1, "a", "completed", "succeeded"), makeRun(2, 2, "b", "completed", "partiallySucceeded"), makeRun(3, 3, "c", "notStarted", ""), makeRun(4, 4, "d", "inProgress", "")},
			want:   BuildRunning,
		},
		{
			name:   "queued beats partial passed",
			checks: []Run{makeRun(1, 1, "a", "completed", "succeeded"), makeRun(2, 2, "b", "completed", "partiallySucceeded"), makeRun(3, 3, "c", "notStarted", "")},
			want:   BuildQueued,
		},
		{
			name:   "partial beats passed",
			checks: []Run{makeRun(1, 1, "a", "completed", "succeeded"), makeRun(2, 2, "b", "completed", "partiallySucceeded")},
			want:   BuildPartial,
		},
		{
			name:   "passed when all succeeded",
			checks: []Run{makeRun(1, 1, "a", "completed", "succeeded")},
			want:   BuildPassed,
		},
		{
			name:   "canceled run alone is BuildNone",
			checks: []Run{makeRun(1, 1, "a", "completed", "canceled")},
			want:   BuildNone,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := SummarizeCI(nil, tc.checks, nil, nil)
			if s.State != tc.want {
				t.Fatalf("got state %v, want %v", s.State, tc.want)
			}
		})
	}
}

func TestManualQueuedPolicyNotActiveOrCancellable(t *testing.T) {
	manualPolicy := makeBuildPolicy("e-manual", "queued", "manual-build", 50, 0, true, true)
	s := SummarizeCI([]Policy{manualPolicy}, nil, nil, nil)
	if s.State != BuildQueued || s.Queued != 1 {
		t.Fatalf("manual queued policy should have BuildQueued state, got %+v", s)
	}
	if s.HasActiveRun() {
		t.Fatal("manual queued policy with BuildID == 0 must not count as HasActiveRun()")
	}
	if s.Checks[0].IsCancellable() || s.Checks[0].HasRun() {
		t.Fatalf("manual queued policy must not be cancellable or have run: %+v", s.Checks[0])
	}

	queuedRun := makeRun(77, 51, "queued-run", "notStarted", "")
	s2 := SummarizeCI(nil, []Run{queuedRun}, nil, nil)
	if !s2.HasActiveRun() || !s2.Checks[0].IsActive() || !s2.Checks[0].IsCancellable() {
		t.Fatalf("notStarted run with ID > 0 must be active and cancellable: %+v", s2.Checks[0])
	}
}
