package ado

import "testing"

func TestClassify(t *testing.T) {
	const me = "me"
	pr := func(id, vote int, draft bool) PullRequest {
		return PullRequest{ID: id, IsDraft: draft, Reviewers: []Reviewer{{Identity: Identity{ID: me}, Vote: vote}}}
	}
	reviewing := []PullRequest{
		pr(1, VoteNone, false),
		pr(2, VoteWaitingForAuthor, false),
		pr(3, VoteRejected, true),
		pr(4, VoteNone, true),
		pr(5, VoteApproved, false),
		pr(6, VoteNone, false), // also created by me
	}
	created := []PullRequest{{ID: 6}}

	got := map[SectionKind][]int{}
	for _, s := range Classify(me, reviewing, created) {
		for _, p := range s.PRs {
			got[s.Kind] = append(got[s.Kind], p.ID)
		}
	}
	want := map[SectionKind][]int{
		SectionNeedsReview:      {1},
		SectionWaitingForAuthor: {2, 3},
		SectionAssigned:         {4, 5},
		SectionCreated:          {6},
	}
	for k, w := range want {
		if len(got[k]) != len(w) {
			t.Fatalf("%s: got %v, want %v", k.Title(), got[k], w)
		}
		for i := range w {
			if got[k][i] != w[i] {
				t.Fatalf("%s: got %v, want %v", k.Title(), got[k], w)
			}
		}
	}
}

func TestOrgName(t *testing.T) {
	for in, want := range map[string]string{
		"arbinSW":                        "arbinSW",
		"https://dev.azure.com/arbinSW/": "arbinSW",
		"https://dev.azure.com/arbinSW":  "arbinSW",
	} {
		if got := OrgName(in); got != want {
			t.Errorf("OrgName(%q) = %q, want %q", in, got, want)
		}
	}
}
