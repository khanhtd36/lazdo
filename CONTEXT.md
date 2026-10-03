# lazdo

A terminal view of one person's Azure DevOps pull requests across an organization, modeled on the Azure DevOps "My pull requests" and pull request pages.

## Language

### Dashboard

**Me**:
The signed-in Azure DevOps identity whose pull requests the dashboard shows.
_Avoid_: user, current user

**Section**:
One of the four fixed groups a pull request falls into on the dashboard: Wait for approval, Waiting for author, Assigned to me, Created by me.
_Avoid_: tab, category, list

**Wait for approval**:
Pull requests Me reviews, has not voted on, and that are not drafts.

**Waiting for author**:
Pull requests where Me's vote is waiting for author or rejected.

**Assigned to me**:
Pull requests Me reviews that are in neither of the two sections above, such as drafts and ones Me approved.

**Created by me**:
Pull requests Me opened and does not review. A pull request Me opened and also reviews belongs to a reviewer section instead.

### Review

**Reviewer**:
An identity, person or group, asked to vote on a pull request; either required or optional.

**Vote**:
A reviewer's verdict: approved, approved with suggestions, no vote, waiting for author, or rejected.
_Avoid_: approval (for the general concept), status

**Push**:
One update of a pull request's source branch, possibly carrying several commits. Azure DevOps calls it an iteration or update.
_Avoid_: iteration, update

**Thread**:
A discussion on a pull request: a human comment thread that can be active or resolved, or a system entry such as a push, a vote, or reviewers being added.
_Avoid_: comment (for the whole discussion)

**Policy**:
A branch rule a pull request must satisfy to complete, such as a build, minimum reviewers or required reviewers; blocking or optional.
_Avoid_: check (except in on-screen text copied from Azure DevOps)

### Lifecycle

**Draft**:
A pull request not yet ready for review; publishing it makes it active.

**Complete**:
Merging a pull request into its target branch, which closes it.
_Avoid_: merge (for the whole act), close

**Auto-complete**:
A standing request to complete a pull request as soon as its blocking policies pass.

**Override**:
Completing a pull request despite failing blocking policies, with a stated reason.
_Avoid_: bypass, force

**Abandon**:
Closing a pull request without merging it.

### Visits

**Visit**:
Me opening a pull request's detail, in the browser or in lazdo; Azure DevOps keeps the latest and the previous visit time.
_Avoid_: view, read

**Unvisited**:
A pull request Me has never visited.

**New since last visit**:
Pushes, comments and votes that arrived after Me's last visit.
_Avoid_: unread, updates
