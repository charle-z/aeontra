package catalog

import "encoding/json"

type SourcePublicOSSService interface {
	SourcePublicIssueStatus(upstreamOwner, repo string, number int) (string, error)
	SourcePublicIssueCreatePreview(upstreamOwner, repo, title, description string) (string, error)
	SourcePublicIssueCreate(planID string, approve bool) (string, error)
	SourcePublicForkCreatePreview(upstreamOwner, repo string) (string, error)
	SourcePublicForkCreate(planID string, approve bool) (string, error)
	SourcePublicIssueCommentPreview(upstreamOwner, repo string, number int, comment string) (string, error)
	SourcePublicIssueComment(planID string, approve bool) (string, error)
	SourcePublicReviewReplyPreview(upstreamOwner, repo string, number int, commentID int64, reply string) (string, error)
	SourcePublicReviewReply(planID string, approve bool) (string, error)
	SourceCrossRepoPullRequestCreatePreview(upstreamOwner, repo, head, base, title, description string, draft bool) (string, error)
	SourceCrossRepoPullRequestCreate(planID string, approve bool) (string, error)
	SourcePublicPullRequestStatus(upstreamOwner, repo string, number int) (string, error)
}

func RegisterSourcePublicOSS(register Register, service SourcePublicOSSService) {
	register(Tool{
		Name:        "source_public_issue_status",
		Description: "Read one public upstream issue, its assignees, complete bounded conversation, and linked pull requests. The configured GitHub token is never exposed.",
		InputSchema: publicRepoNumberSchema("issue number"),
		Version:     "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var p struct {
				UpstreamOwner string `json:"upstream_owner"`
				Repo          string `json:"repo"`
				Number        int    `json:"number"`
			}
			if err := json.Unmarshal(arguments, &p); err != nil {
				return "", err
			}
			return service.SourcePublicIssueStatus(p.UpstreamOwner, p.Repo, p.Number)
		},
	})
	register(Tool{
		Name:        "source_public_issue_create_preview",
		Description: "Verify one public external repository and plan creation of one bounded issue with an exact title and body. Nothing is created.",
		InputSchema: closedObject(map[string]any{
			"upstream_owner": boundedStringProp("public external GitHub owner", 1, 39),
			"repo":           boundedStringProp("public repository name", 1, 100),
			"title":          boundedStringProp("exact issue title", 1, 256),
			"description":    boundedStringProp("exact issue body", 1, 8192),
		}, "upstream_owner", "repo", "title", "description"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var p struct {
				UpstreamOwner string `json:"upstream_owner"`
				Repo          string `json:"repo"`
				Title         string `json:"title"`
				Description   string `json:"description"`
			}
			if err := decodeStrict(arguments, &p); err != nil {
				return "", err
			}
			return service.SourcePublicIssueCreatePreview(p.UpstreamOwner, p.Repo, p.Title, p.Description)
		},
	})
	register(Tool{
		Name:        "source_public_issue_create",
		Description: "Execute one reviewed source_public_issue_create_preview plan after revalidating the public upstream repository identity.",
		InputSchema: closedObject(map[string]any{
			"plan_id": boundedStringProp("issue creation plan returned by source_public_issue_create_preview", 1, 128),
			"approve": boolProp("execute when approval is required"),
		}, "plan_id"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var p struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := decodeStrict(arguments, &p); err != nil {
				return "", err
			}
			return service.SourcePublicIssueCreate(p.PlanID, p.Approve)
		},
	})
	register(Tool{
		Name:        "source_public_fork_create_preview",
		Description: "Verify one public external repository and the configured owner's fork state, then create an exact expiring single-use fork plan. Nothing is created.",
		InputSchema: publicRepoSchema(),
		Version:     "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var p struct {
				UpstreamOwner string `json:"upstream_owner"`
				Repo          string `json:"repo"`
			}
			if err := json.Unmarshal(arguments, &p); err != nil {
				return "", err
			}
			return service.SourcePublicForkCreatePreview(p.UpstreamOwner, p.Repo)
		},
	})
	register(Tool{
		Name:        "source_public_fork_create",
		Description: "Execute one reviewed source_public_fork_create_preview plan after revalidating the upstream and fork absence. The token is never exposed.",
		InputSchema: planExecutionSchema("fork plan returned by source_public_fork_create_preview"),
		Version:     "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var p struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(arguments, &p); err != nil {
				return "", err
			}
			return service.SourcePublicForkCreate(p.PlanID, p.Approve)
		},
	})
	register(Tool{
		Name:        "source_public_issue_comment_preview",
		Description: "Read and freeze one open public issue or pull-request conversation before planning one bounded comment. Nothing is posted.",
		InputSchema: closedObject(map[string]any{
			"upstream_owner": boundedStringProp("public external GitHub owner", 1, 39),
			"repo":           boundedStringProp("public repository name", 1, 100),
			"number":         integerProp("issue or pull request number", 1, 1000000),
			"comment":        boundedStringProp("exact public comment body", 1, 4096),
		}, "upstream_owner", "repo", "number", "comment"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var p struct {
				UpstreamOwner string `json:"upstream_owner"`
				Repo          string `json:"repo"`
				Number        int    `json:"number"`
				Comment       string `json:"comment"`
			}
			if err := json.Unmarshal(arguments, &p); err != nil {
				return "", err
			}
			return service.SourcePublicIssueCommentPreview(p.UpstreamOwner, p.Repo, p.Number, p.Comment)
		},
	})
	register(Tool{
		Name:        "source_public_issue_comment",
		Description: "Execute one reviewed source_public_issue_comment_preview plan after requiring the issue conversation to remain unchanged.",
		InputSchema: planExecutionSchema("comment plan returned by source_public_issue_comment_preview"),
		Version:     "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var p struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(arguments, &p); err != nil {
				return "", err
			}
			return service.SourcePublicIssueComment(p.PlanID, p.Approve)
		},
	})
	register(Tool{
		Name:        "source_public_review_reply_preview",
		Description: "Read and freeze one exact public pull-request review comment before planning a threaded reply. Nothing is posted.",
		InputSchema: closedObject(map[string]any{
			"upstream_owner": boundedStringProp("public external GitHub owner", 1, 39),
			"repo":           boundedStringProp("public repository name", 1, 100),
			"number":         integerProp("pull request number", 1, 1000000),
			"comment_id":     integerProp("review comment identifier", 1, 9007199254740991),
			"reply":          boundedStringProp("exact threaded reply body", 1, 4096),
		}, "upstream_owner", "repo", "number", "comment_id", "reply"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var p struct {
				UpstreamOwner string `json:"upstream_owner"`
				Repo          string `json:"repo"`
				Number        int    `json:"number"`
				CommentID     int64  `json:"comment_id"`
				Reply         string `json:"reply"`
			}
			if err := json.Unmarshal(arguments, &p); err != nil {
				return "", err
			}
			return service.SourcePublicReviewReplyPreview(p.UpstreamOwner, p.Repo, p.Number, p.CommentID, p.Reply)
		},
	})
	register(Tool{
		Name:        "source_public_review_reply",
		Description: "Execute one reviewed source_public_review_reply_preview plan after revalidating the pull-request head and review comment timestamp.",
		InputSchema: planExecutionSchema("review reply plan returned by source_public_review_reply_preview"),
		Version:     "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var p struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(arguments, &p); err != nil {
				return "", err
			}
			return service.SourcePublicReviewReply(p.PlanID, p.Approve)
		},
	})
	register(Tool{
		Name:        "source_cross_repo_pull_request_create_preview",
		Description: "Verify an exact configured-owner fork branch, upstream base, ancestry, and duplicate state before planning one cross-repository pull request. Nothing is created.",
		InputSchema: closedObject(map[string]any{
			"upstream_owner": boundedStringProp("public external GitHub owner", 1, 39),
			"repo":           boundedStringProp("fork and upstream repository name", 1, 100),
			"head":           boundedStringProp("source branch in the configured owner's fork", 1, 128),
			"base":           boundedStringProp("target branch in the upstream repository", 1, 128),
			"title":          boundedStringProp("pull request title", 1, 256),
			"description":    boundedStringProp("optional pull request body", 0, 8192),
			"draft":          boolProp("create the pull request as a draft"),
		}, "upstream_owner", "repo", "head", "base", "title"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var p struct {
				UpstreamOwner string `json:"upstream_owner"`
				Repo          string `json:"repo"`
				Head          string `json:"head"`
				Base          string `json:"base"`
				Title         string `json:"title"`
				Description   string `json:"description"`
				Draft         bool   `json:"draft"`
			}
			if err := json.Unmarshal(arguments, &p); err != nil {
				return "", err
			}
			return service.SourceCrossRepoPullRequestCreatePreview(p.UpstreamOwner, p.Repo, p.Head, p.Base, p.Title, p.Description, p.Draft)
		},
	})
	register(Tool{
		Name:        "source_cross_repo_pull_request_create",
		Description: "Execute one reviewed source_cross_repo_pull_request_create_preview plan after revalidating fork, branch SHAs, ancestry, and duplicate state.",
		InputSchema: planExecutionSchema("pull request plan returned by source_cross_repo_pull_request_create_preview"),
		Version:     "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var p struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(arguments, &p); err != nil {
				return "", err
			}
			return service.SourceCrossRepoPullRequestCreate(p.PlanID, p.Approve)
		},
	})
	register(Tool{
		Name:        "source_public_pull_request_status",
		Description: "Read one public upstream pull request, its exact fork head checks, reviews, and conversation comments. Output is bounded and redacted.",
		InputSchema: publicRepoNumberSchema("pull request number"),
		Version:     "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var p struct {
				UpstreamOwner string `json:"upstream_owner"`
				Repo          string `json:"repo"`
				Number        int    `json:"number"`
			}
			if err := json.Unmarshal(arguments, &p); err != nil {
				return "", err
			}
			return service.SourcePublicPullRequestStatus(p.UpstreamOwner, p.Repo, p.Number)
		},
	})
}

func publicRepoSchema() map[string]any {
	return closedObject(map[string]any{
		"upstream_owner": boundedStringProp("public external GitHub owner", 1, 39),
		"repo":           boundedStringProp("public repository name", 1, 100),
	}, "upstream_owner", "repo")
}

func publicRepoNumberSchema(description string) map[string]any {
	return closedObject(map[string]any{
		"upstream_owner": boundedStringProp("public external GitHub owner", 1, 39),
		"repo":           boundedStringProp("public repository name", 1, 100),
		"number":         integerProp(description, 1, 1000000),
	}, "upstream_owner", "repo", "number")
}

func planExecutionSchema(description string) map[string]any {
	return closedObject(map[string]any{
		"plan_id": strProp(description),
		"approve": boolProp("execute when approval is required"),
	}, "plan_id")
}
