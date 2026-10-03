package ado

const (
	VoteApproved             = 10
	VoteApprovedWithSuggests = 5
	VoteNone                 = 0
	VoteWaitingForAuthor     = -5
	VoteRejected             = -10
)

type SectionKind int

const (
	SectionNeedsReview SectionKind = iota
	SectionWaitingForAuthor
	SectionAssigned
	SectionCreated
	sectionCount
)

func (k SectionKind) Title() string {
	switch k {
	case SectionNeedsReview:
		return "Wait for approval"
	case SectionWaitingForAuthor:
		return "Waiting for author"
	case SectionAssigned:
		return "Assigned to me"
	case SectionCreated:
		return "Created by me"
	default:
		return "?"
	}
}

type Section struct {
	Kind SectionKind
	PRs  []PullRequest
}

// Classify sorts PRs into the dashboard sections, matching the Azure DevOps
// "My pull requests" page:
//   - Wait for approval: I'm a reviewer, haven't voted, PR is not a draft.
//   - Waiting for author: my vote is "waiting for author" or "rejected".
//   - Assigned to me: every other PR I review (drafts, ones I approved).
//   - Created by me: PRs I opened but don't review; a PR I both opened and
//     review goes to one of the reviewer sections instead.
func Classify(meID string, reviewing, created []PullRequest) []Section {
	sections := make([]Section, sectionCount)
	for i := range sections {
		sections[i].Kind = SectionKind(i)
	}
	reviewed := make(map[int]bool, len(reviewing))
	for _, pr := range reviewing {
		reviewed[pr.ID] = true
		kind := SectionAssigned
		if me, ok := pr.ReviewerFor(meID); ok {
			switch {
			case me.Vote <= VoteWaitingForAuthor:
				kind = SectionWaitingForAuthor
			case me.Vote == VoteNone && !pr.IsDraft:
				kind = SectionNeedsReview
			}
		}
		sections[kind].PRs = append(sections[kind].PRs, pr)
	}
	for _, pr := range created {
		if !reviewed[pr.ID] {
			sections[SectionCreated].PRs = append(sections[SectionCreated].PRs, pr)
		}
	}
	return sections
}
