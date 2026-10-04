# lazdo

A terminal view of one person's Azure DevOps pull requests across an organization, modeled on the Azure DevOps "My pull requests" and pull request pages.

## Language

### Dashboard

**Page**:
One of the dashboard's top-level views: Pull requests (Me's pull requests in Sections) or Projects.
_Avoid_: tab (reserved for the views inside a pull request or project), screen

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

### Projects

**Project**:
An Azure DevOps project in the organization, holding repos, pull requests and pipelines.

**Recent project**:
A project Me visited lately, in the order Azure DevOps remembers them.

**Active project**:
A project with at least one active pull request, by anyone.

**Repo**:
A git repository in a project.
_Avoid_: repository (in on-screen text), repo URL (ambiguous: clone or web)

**Branch**:
A branch of a repo, compared against the repo's default branch as ahead and behind counts.

**Commit**:
One recorded change to a repo, shown in a branch's history or a tag's release changes.

**Tag**:
A name fixed to one commit, usually a version. Annotated tags also carry a tagger, date and message; lightweight tags are only the name.
_Avoid_: label (Azure DevOps uses labels for pull request tags)

**Release changes**:
The commits a tag adds over the previous tag by version order.
_Avoid_: changelog, diff (for the commit list)

**Pipeline**:
A build pipeline definition in a project, YAML or classic.
_Avoid_: build definition, release (release pipelines are out of scope)

**Run**:
One execution of a pipeline, made of stages, jobs and steps, each with a status and a log.
_Avoid_: build (for the execution)

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

**Line comment**:
A thread anchored to a line range on one side, old or new, of a changed file.
_Avoid_: inline comment, code comment

**Comparison**:
The two versions a diff shows: the merge base or an earlier push against a later push, or one commit against its parent.
_Avoid_: diff (for the choice of versions), iteration range

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
