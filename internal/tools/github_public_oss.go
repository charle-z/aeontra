package tools

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

func (s *SourceCapability) validatePublicUpstream(owner, repo string) error {
	if err := s.github.configError(); err != nil {
		return err
	}
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	if !safeGitHubOwner(owner) || !safeCloneDir(repo) {
		return fmt.Errorf("invalid public GitHub owner or repository")
	}
	if strings.EqualFold(owner, s.github.owner) {
		return fmt.Errorf("public OSS operation requires an external upstream owner")
	}
	return nil
}

func publicText(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

func issueAssignees(issue githubPublicIssueResponse) string {
	var values []string
	for _, assignee := range issue.Assignees {
		if login := strings.TrimSpace(assignee.Login); login != "" {
			values = append(values, login)
		}
	}
	if len(values) == 0 {
		return "none"
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}

func linkedPullRequests(events []githubPublicTimelineEvent) []string {
	seen := map[string]struct{}{}
	for _, event := range events {
		if event.Source == nil || len(event.Source.Issue.PullRequest) == 0 {
			continue
		}
		value := strings.TrimSpace(event.Source.Issue.HTMLURL)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	values := make([]string, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func (s *SourceCapability) SourcePublicIssueStatus(upstreamOwner, repo string, number int) (string, error) {
	sp := s.log.Start("source_public_issue_status")
	if err := s.validatePublicUpstream(upstreamOwner, repo); err != nil {
		sp.Finish(audit.Deny, "status", nil, err)
		return "", err
	}
	if number < 1 {
		return "", fmt.Errorf("invalid issue number")
	}
	ctx := context.Background()
	if _, err := s.github.publicRepo(ctx, upstreamOwner, repo); err != nil {
		return "", err
	}
	issue, err := s.github.publicIssue(ctx, upstreamOwner, repo, number)
	if err != nil {
		return "", err
	}
	comments, err := s.github.publicIssueComments(ctx, upstreamOwner, repo, number)
	if err != nil {
		return "", err
	}
	timeline, err := s.github.publicIssueTimeline(ctx, upstreamOwner, repo, number)
	if err != nil {
		return "", err
	}
	linked := linkedPullRequests(timeline)
	var b strings.Builder
	fmt.Fprintf(&b, "repository: %s/%s\nissue: %d\nurl: %s\nstate: %s\ntitle: %s\nupdated_at: %s\nassignees: %s\ncomments: %d\nlinked_pull_requests: %d\n", upstreamOwner, repo, number, issue.HTMLURL, issue.State, publicText(issue.Title, 512), issue.UpdatedAt, issueAssignees(issue), len(comments), len(linked))
	for _, comment := range comments {
		fmt.Fprintf(&b, "comment: %s | created_at=%s | body=%s\n", comment.User.Login, comment.CreatedAt, publicText(comment.Body, 1000))
	}
	for _, value := range linked {
		fmt.Fprintf(&b, "linked_pull_request: %s\n", value)
	}
	sp.Finish(audit.Allow, upstreamOwner+"/"+repo+" #"+strconv.Itoa(number), nil, nil)
	return s.redact(b.String()), nil
}

func (s *SourceCapability) SourcePublicForkCreatePreview(upstreamOwner, repo string) (string, error) {
	sp := s.log.Start("source_public_fork_create_preview")
	if err := s.validatePublicUpstream(upstreamOwner, repo); err != nil {
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	ctx := context.Background()
	upstream, err := s.github.publicRepo(ctx, upstreamOwner, repo)
	if err != nil {
		return "", err
	}
	fork, exists, err := s.github.configuredFork(ctx, upstreamOwner, repo)
	if err != nil {
		return "", err
	}
	if exists {
		return fmt.Sprintf("fork: %s\nparent: %s/%s\nexists: true\ndefault_branch: %s\n", fork.FullName, upstreamOwner, repo, fork.DefaultBranch), nil
	}
	plan, err := s.plans.Create("source-public-fork-create", map[string]string{
		"upstream_owner": upstreamOwner,
		"repo":           repo,
		"upstream":       upstream.FullName,
		"default_branch": upstream.DefaultBranch,
		"fork_owner":     s.github.owner,
	})
	if err != nil {
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+upstream.FullName+" "+plan.ID, nil, nil)
	return fmt.Sprintf("upstream: %s\nfork: %s/%s\nexists: false\ndefault_branch: %s\neffect: create one public fork under the configured owner\nplan_id: %s\nexpiry: %s\n", upstream.FullName, s.github.owner, repo, upstream.DefaultBranch, plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *SourceCapability) SourcePublicForkCreate(planID string, approve bool) (string, error) {
	sp := s.log.Start("source_public_fork_create")
	if err := s.github.configError(); err != nil {
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: source_public_fork_create would execute the reviewed single-use plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "source-public-fork-create")
	if err != nil {
		return "", err
	}
	if plan.Args["fork_owner"] != s.github.owner {
		return "", fmt.Errorf("configured GitHub owner changed after preview")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	upstream, err := s.github.publicRepo(ctx, plan.Args["upstream_owner"], plan.Args["repo"])
	if err != nil || upstream.FullName != plan.Args["upstream"] || upstream.DefaultBranch != plan.Args["default_branch"] {
		return "", fmt.Errorf("upstream repository changed after preview")
	}
	if _, exists, err := s.github.configuredFork(ctx, plan.Args["upstream_owner"], plan.Args["repo"]); err != nil {
		return "", err
	} else if exists {
		return "", fmt.Errorf("fork appeared after preview")
	}
	if _, err := s.github.createFork(ctx, plan.Args["upstream_owner"], plan.Args["repo"]); err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	fork, err := s.github.waitForConfiguredFork(ctx, plan.Args["upstream_owner"], plan.Args["repo"])
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, planID, nil, nil)
	return fmt.Sprintf("fork: %s\nparent: %s\ndefault_branch: %s\npublic: true\nwritable: true\n", fork.FullName, plan.Args["upstream"], fork.DefaultBranch), nil
}

func (s *SourceCapability) SourcePublicIssueCommentPreview(upstreamOwner, repo string, number int, comment string) (string, error) {
	sp := s.log.Start("source_public_issue_comment_preview")
	if err := s.validatePublicUpstream(upstreamOwner, repo); err != nil {
		return "", err
	}
	comment = s.redact(strings.TrimSpace(comment))
	if number < 1 || comment == "" || len(comment) > 4096 {
		return "", fmt.Errorf("invalid issue number or comment body")
	}
	ctx := context.Background()
	if _, err := s.github.publicRepo(ctx, upstreamOwner, repo); err != nil {
		return "", err
	}
	issue, err := s.github.publicIssue(ctx, upstreamOwner, repo, number)
	if err != nil {
		return "", err
	}
	if issue.State != "open" {
		return "", fmt.Errorf("issue is not open")
	}
	comments, err := s.github.publicIssueComments(ctx, upstreamOwner, repo, number)
	if err != nil {
		return "", err
	}
	for _, existing := range comments {
		if existing.User.Login == s.github.owner && strings.TrimSpace(existing.Body) == comment {
			return "", fmt.Errorf("identical issue comment already exists")
		}
	}
	plan, err := s.plans.Create("source-public-issue-comment", map[string]string{
		"upstream_owner": upstreamOwner,
		"repo":           repo,
		"number":         strconv.Itoa(number),
		"comment":        comment,
		"updated_at":     issue.UpdatedAt,
		"comments":       strconv.Itoa(issue.Comments),
	})
	if err != nil {
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+upstreamOwner+"/"+repo+" #"+strconv.Itoa(number), nil, nil)
	return fmt.Sprintf("repository: %s/%s\nissue: %d\nstate: open\ncomment: %s\neffect: create one issue or pull-request conversation comment\nplan_id: %s\nexpiry: %s\n", upstreamOwner, repo, number, publicText(comment, 1000), plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *SourceCapability) SourcePublicIssueComment(planID string, approve bool) (string, error) {
	sp := s.log.Start("source_public_issue_comment")
	if err := s.github.configError(); err != nil {
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: source_public_issue_comment would execute the reviewed single-use plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "source-public-issue-comment")
	if err != nil {
		return "", err
	}
	number, _ := strconv.Atoi(plan.Args["number"])
	ctx := context.Background()
	if _, err := s.github.publicRepo(ctx, plan.Args["upstream_owner"], plan.Args["repo"]); err != nil {
		return "", err
	}
	issue, err := s.github.publicIssue(ctx, plan.Args["upstream_owner"], plan.Args["repo"], number)
	if err != nil {
		return "", err
	}
	if issue.State != "open" || issue.UpdatedAt != plan.Args["updated_at"] || strconv.Itoa(issue.Comments) != plan.Args["comments"] {
		return "", fmt.Errorf("issue state changed after preview")
	}
	comments, err := s.github.publicIssueComments(ctx, plan.Args["upstream_owner"], plan.Args["repo"], number)
	if err != nil {
		return "", err
	}
	for _, existing := range comments {
		if existing.User.Login == s.github.owner && strings.TrimSpace(existing.Body) == plan.Args["comment"] {
			return "", fmt.Errorf("identical issue comment appeared after preview")
		}
	}
	created, err := s.github.createPublicIssueComment(ctx, plan.Args["upstream_owner"], plan.Args["repo"], number, plan.Args["comment"])
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, planID, nil, nil)
	return fmt.Sprintf("repository: %s/%s\nissue: %d\nissue_comment: %d\nurl: %s\n", plan.Args["upstream_owner"], plan.Args["repo"], number, created.ID, created.HTMLURL), nil
}

func (s *SourceCapability) SourceCrossRepoPullRequestCreatePreview(upstreamOwner, repo, head, base, title, description string, draft bool) (string, error) {
	sp := s.log.Start("source_cross_repo_pull_request_create_preview")
	if err := s.validatePublicUpstream(upstreamOwner, repo); err != nil {
		return "", err
	}
	if !safeGitHubRef(head) || !safeGitHubRef(base) {
		return "", fmt.Errorf("invalid GitHub head or base ref")
	}
	title = s.redact(strings.TrimSpace(title))
	description = s.redact(strings.TrimSpace(description))
	if title == "" || len(title) > 256 || len(description) > 8192 {
		return "", fmt.Errorf("pull request title/body exceeds bounds or title is empty")
	}
	ctx := context.Background()
	upstream, err := s.github.publicRepo(ctx, upstreamOwner, repo)
	if err != nil {
		return "", err
	}
	if _, exists, err := s.github.configuredFork(ctx, upstreamOwner, repo); err != nil {
		return "", err
	} else if !exists {
		return "", fmt.Errorf("configured owner fork does not exist")
	}
	headSHA, err := s.github.branchSHAAt(ctx, s.github.owner, repo, head)
	if err != nil {
		return "", err
	}
	baseSHA, err := s.github.branchSHAAt(ctx, upstreamOwner, repo, base)
	if err != nil {
		return "", err
	}
	relation, err := s.github.compareAt(ctx, s.github.owner, repo, baseSHA, headSHA)
	if err != nil {
		return "", err
	}
	if relation != "ahead" {
		return "", fmt.Errorf("fork head is not ahead of the upstream base (status=%s)", relation)
	}
	existing, err := s.github.findCrossRepoPullRequest(ctx, upstreamOwner, repo, head, base)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return fmt.Sprintf("pull_request: %d\nurl: %s\nstate: open\nexisting: true\n", existing.Number, existing.HTMLURL), fmt.Errorf("an open pull request already exists")
	}
	plan, err := s.plans.Create("source-cross-repo-pr-create", map[string]string{
		"upstream_owner": upstreamOwner,
		"repo":           repo,
		"upstream":       upstream.FullName,
		"fork_owner":     s.github.owner,
		"head":           head,
		"base":           base,
		"head_sha":       headSHA,
		"base_sha":       baseSHA,
		"title":          title,
		"description":    description,
		"draft":          strconv.FormatBool(draft),
	})
	if err != nil {
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+upstream.FullName+" "+plan.ID, nil, nil)
	return fmt.Sprintf("upstream: %s\nhead: %s:%s\nhead_sha: %s\nbase: %s\nbase_sha: %s\ndraft: %t\neffect: create one cross-repository pull request\nplan_id: %s\nexpiry: %s\n", upstream.FullName, s.github.owner, head, headSHA, base, baseSHA, draft, plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *SourceCapability) SourceCrossRepoPullRequestCreate(planID string, approve bool) (string, error) {
	sp := s.log.Start("source_cross_repo_pull_request_create")
	if err := s.github.configError(); err != nil {
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: source_cross_repo_pull_request_create would execute the reviewed single-use plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "source-cross-repo-pr-create")
	if err != nil {
		return "", err
	}
	if plan.Args["fork_owner"] != s.github.owner {
		return "", fmt.Errorf("configured GitHub owner changed after preview")
	}
	ctx := context.Background()
	upstream, err := s.github.publicRepo(ctx, plan.Args["upstream_owner"], plan.Args["repo"])
	if err != nil || upstream.FullName != plan.Args["upstream"] {
		return "", fmt.Errorf("upstream repository changed after preview")
	}
	if _, exists, err := s.github.configuredFork(ctx, plan.Args["upstream_owner"], plan.Args["repo"]); err != nil {
		return "", err
	} else if !exists {
		return "", fmt.Errorf("configured owner fork disappeared after preview")
	}
	headSHA, err := s.github.branchSHAAt(ctx, s.github.owner, plan.Args["repo"], plan.Args["head"])
	if err != nil || headSHA != plan.Args["head_sha"] {
		return "", fmt.Errorf("pull request head changed after preview")
	}
	baseSHA, err := s.github.branchSHAAt(ctx, plan.Args["upstream_owner"], plan.Args["repo"], plan.Args["base"])
	if err != nil || baseSHA != plan.Args["base_sha"] {
		return "", fmt.Errorf("pull request base changed after preview")
	}
	relation, err := s.github.compareAt(ctx, s.github.owner, plan.Args["repo"], baseSHA, headSHA)
	if err != nil || relation != "ahead" {
		return "", fmt.Errorf("pull request ancestry changed after preview")
	}
	existing, err := s.github.findCrossRepoPullRequest(ctx, plan.Args["upstream_owner"], plan.Args["repo"], plan.Args["head"], plan.Args["base"])
	if err != nil {
		return "", err
	}
	if existing != nil {
		return "", fmt.Errorf("an open pull request appeared after preview")
	}
	draft, _ := strconv.ParseBool(plan.Args["draft"])
	pull, err := s.github.createCrossRepoPullRequest(ctx, plan.Args["upstream_owner"], plan.Args["repo"], plan.Args["head"], plan.Args["base"], plan.Args["title"], plan.Args["description"], draft)
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, planID, nil, nil)
	return fmt.Sprintf("repository: %s\npull_request: %d\nurl: %s\nstate: %s\nhead_sha: %s\nbase: %s\ndraft: %t\n", plan.Args["upstream"], pull.Number, pull.HTMLURL, pull.State, pull.Head.SHA, pull.Base.Ref, draft), nil
}

func reviewCommentMatchesPull(comment githubPublicReviewCommentResponse, owner, repo string, number int) bool {
	suffix := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number)
	return strings.HasSuffix(strings.TrimRight(comment.PullRequestURL, "/"), suffix)
}

func (s *SourceCapability) SourcePublicReviewReplyPreview(upstreamOwner, repo string, number int, commentID int64, reply string) (string, error) {
	sp := s.log.Start("source_public_review_reply_preview")
	if err := s.validatePublicUpstream(upstreamOwner, repo); err != nil {
		return "", err
	}
	reply = s.redact(strings.TrimSpace(reply))
	if number < 1 || commentID < 1 || reply == "" || len(reply) > 4096 {
		return "", fmt.Errorf("invalid pull request, review comment, or reply body")
	}
	ctx := context.Background()
	if _, err := s.github.publicRepo(ctx, upstreamOwner, repo); err != nil {
		return "", err
	}
	pull, err := s.github.publicPullRequest(ctx, upstreamOwner, repo, number)
	if err != nil {
		return "", err
	}
	if pull.State != "open" || pull.Merged {
		return "", fmt.Errorf("pull request is not open")
	}
	comment, err := s.github.publicReviewComment(ctx, upstreamOwner, repo, commentID)
	if err != nil {
		return "", err
	}
	if !reviewCommentMatchesPull(comment, upstreamOwner, repo, number) {
		return "", fmt.Errorf("review comment does not belong to the selected pull request")
	}
	plan, err := s.plans.Create("source-public-review-reply", map[string]string{
		"upstream_owner":  upstreamOwner,
		"repo":            repo,
		"number":          strconv.Itoa(number),
		"comment_id":      strconv.FormatInt(commentID, 10),
		"comment_updated": comment.UpdatedAt,
		"pull_head_sha":   pull.Head.SHA,
		"reply":           reply,
	})
	if err != nil {
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+upstreamOwner+"/"+repo+" #"+strconv.Itoa(number), nil, nil)
	location := comment.Path
	if comment.Line > 0 {
		location += ":" + strconv.Itoa(comment.Line)
	}
	return fmt.Sprintf("repository: %s/%s\npull_request: %d\nreview_comment: %d\nauthor: %s\nlocation: %s\ncomment: %s\nreply: %s\neffect: reply to one exact review comment\nplan_id: %s\nexpiry: %s\n", upstreamOwner, repo, number, commentID, comment.User.Login, location, publicText(comment.Body, 1000), publicText(reply, 1000), plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *SourceCapability) SourcePublicReviewReply(planID string, approve bool) (string, error) {
	sp := s.log.Start("source_public_review_reply")
	if err := s.github.configError(); err != nil {
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: source_public_review_reply would execute the reviewed single-use plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "source-public-review-reply")
	if err != nil {
		return "", err
	}
	number, _ := strconv.Atoi(plan.Args["number"])
	commentID, _ := strconv.ParseInt(plan.Args["comment_id"], 10, 64)
	ctx := context.Background()
	if _, err := s.github.publicRepo(ctx, plan.Args["upstream_owner"], plan.Args["repo"]); err != nil {
		return "", err
	}
	pull, err := s.github.publicPullRequest(ctx, plan.Args["upstream_owner"], plan.Args["repo"], number)
	if err != nil {
		return "", err
	}
	if pull.State != "open" || pull.Merged || pull.Head.SHA != plan.Args["pull_head_sha"] {
		return "", fmt.Errorf("pull request changed after preview")
	}
	comment, err := s.github.publicReviewComment(ctx, plan.Args["upstream_owner"], plan.Args["repo"], commentID)
	if err != nil {
		return "", err
	}
	if !reviewCommentMatchesPull(comment, plan.Args["upstream_owner"], plan.Args["repo"], number) || comment.UpdatedAt != plan.Args["comment_updated"] {
		return "", fmt.Errorf("review comment changed after preview")
	}
	created, err := s.github.createPublicReviewReply(ctx, plan.Args["upstream_owner"], plan.Args["repo"], number, commentID, plan.Args["reply"])
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, planID, nil, nil)
	return fmt.Sprintf("repository: %s/%s\npull_request: %d\nreview_comment: %d\nreview_reply: %d\nurl: %s\n", plan.Args["upstream_owner"], plan.Args["repo"], number, commentID, created.ID, created.HTMLURL), nil
}

func (s *SourceCapability) SourcePublicPullRequestStatus(upstreamOwner, repo string, number int) (string, error) {
	sp := s.log.Start("source_public_pull_request_status")
	if err := s.validatePublicUpstream(upstreamOwner, repo); err != nil {
		return "", err
	}
	if number < 1 {
		return "", fmt.Errorf("invalid pull request number")
	}
	ctx := context.Background()
	if _, err := s.github.publicRepo(ctx, upstreamOwner, repo); err != nil {
		return "", err
	}
	pull, err := s.github.publicPullRequest(ctx, upstreamOwner, repo, number)
	if err != nil {
		return "", err
	}
	if pull.Head.Repo.FullName == "" {
		return "", fmt.Errorf("pull request head repository is unavailable")
	}
	summary, headErr := s.github.publicCheckSummary(ctx, pull.Head.Repo.FullName, pull.Head.SHA)
	if headErr != nil || !summary.EvidenceComplete {
		upstreamSummary, upstreamErr := s.github.publicCheckSummary(ctx, upstreamOwner+"/"+repo, pull.Head.SHA)
		if upstreamErr == nil && upstreamSummary.EvidenceComplete {
			summary = upstreamSummary
			headErr = nil
		}
	}
	if headErr != nil {
		return "", headErr
	}
	reviews, err := s.github.publicPullReviews(ctx, upstreamOwner, repo, number)
	if err != nil {
		return "", err
	}
	reviewComments, err := s.github.publicReviewComments(ctx, upstreamOwner, repo, number)
	if err != nil {
		return "", err
	}
	comments, err := s.github.publicIssueComments(ctx, upstreamOwner, repo, number)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "repository: %s/%s\npull_request: %d\nurl: %s\nstate: %s\nmerged: %t\nhead: %s:%s\nhead_sha: %s\nbase: %s\nmergeable: %s\nsource: %s\nruns_total: %d\npassed: %d\npending: %d\nfailed: %d\ncommit_statuses: %d\nall_checks_green: %t\nevidence_complete: %t\nreviews: %d\nreview_comments: %d\nconversation_comments: %d\n", upstreamOwner, repo, number, pull.HTMLURL, pull.State, pull.Merged, pull.Head.User.Login, pull.Head.Ref, pull.Head.SHA, pull.Base.Ref, nullableBool(pull.Mergeable), summary.Source, summary.RunsTotal, summary.Passed, summary.Pending, summary.Failed, summary.CommitStatuses, summary.AllChecksGreen, summary.EvidenceComplete, len(reviews), len(reviewComments), len(comments))
	for _, line := range summary.Lines {
		b.WriteString(line + "\n")
	}
	for _, review := range reviews {
		fmt.Fprintf(&b, "review: %s | state=%s | submitted_at=%s | body=%s\n", review.User.Login, review.State, review.SubmittedAt, publicText(review.Body, 1000))
	}
	for _, comment := range reviewComments {
		fmt.Fprintf(&b, "review_comment: %d | author=%s | path=%s | line=%d | in_reply_to=%d | created_at=%s | body=%s\n", comment.ID, comment.User.Login, comment.Path, comment.Line, comment.InReplyToID, comment.CreatedAt, publicText(comment.Body, 1000))
	}
	for _, comment := range comments {
		fmt.Fprintf(&b, "conversation_comment: %s | created_at=%s | body=%s\n", comment.User.Login, comment.CreatedAt, publicText(comment.Body, 1000))
	}
	sp.Finish(audit.Allow, upstreamOwner+"/"+repo+" #"+strconv.Itoa(number), nil, nil)
	return s.redact(b.String()), nil
}
