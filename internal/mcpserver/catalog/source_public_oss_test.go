package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakeSourcePublicOSSService struct {
	calls []string
}

func (f *fakeSourcePublicOSSService) SourcePublicIssueStatus(owner, repo string, number int) (string, error) {
	f.calls = append(f.calls, "issue-status:"+owner+":"+repo)
	return "issue-status", nil
}
func (f *fakeSourcePublicOSSService) SourcePublicForkCreatePreview(owner, repo string) (string, error) {
	f.calls = append(f.calls, "fork-preview:"+owner+":"+repo)
	return "fork-preview", nil
}
func (f *fakeSourcePublicOSSService) SourcePublicForkCreate(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "fork-create:"+planID)
	return "fork-create", nil
}
func (f *fakeSourcePublicOSSService) SourcePublicIssueCommentPreview(owner, repo string, number int, comment string) (string, error) {
	f.calls = append(f.calls, "comment-preview:"+comment)
	return "comment-preview", nil
}
func (f *fakeSourcePublicOSSService) SourcePublicIssueComment(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "comment-create:"+planID)
	return "comment-create", nil
}
func (f *fakeSourcePublicOSSService) SourcePublicReviewReplyPreview(owner, repo string, number int, commentID int64, reply string) (string, error) {
	f.calls = append(f.calls, "review-reply-preview:"+reply)
	return "review-reply-preview", nil
}
func (f *fakeSourcePublicOSSService) SourcePublicReviewReply(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "review-reply:"+planID)
	return "review-reply", nil
}
func (f *fakeSourcePublicOSSService) SourceCrossRepoPullRequestCreatePreview(owner, repo, head, base, title, description string, draft bool) (string, error) {
	f.calls = append(f.calls, "pr-preview:"+owner+":"+repo+":"+head+":"+base)
	return "pr-preview", nil
}
func (f *fakeSourcePublicOSSService) SourceCrossRepoPullRequestCreate(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "pr-create:"+planID)
	return "pr-create", nil
}
func (f *fakeSourcePublicOSSService) SourcePublicPullRequestStatus(owner, repo string, number int) (string, error) {
	f.calls = append(f.calls, "pr-status:"+owner+":"+repo)
	return "pr-status", nil
}

func TestRegisterSourcePublicOSSDefinesStableContractsAndRoutesHandlers(t *testing.T) {
	service := &fakeSourcePublicOSSService{}
	var registered []Tool
	RegisterSourcePublicOSS(func(tool Tool) { registered = append(registered, tool) }, service)

	var names []string
	byName := map[string]Tool{}
	for _, tool := range registered {
		names = append(names, tool.Name)
		byName[tool.Name] = tool
		if tool.Version != "1" {
			t.Fatalf("tool %s version=%q", tool.Name, tool.Version)
		}
	}
	want := []string{
		"source_public_issue_status",
		"source_public_fork_create_preview",
		"source_public_fork_create",
		"source_public_issue_comment_preview",
		"source_public_issue_comment",
		"source_public_review_reply_preview",
		"source_public_review_reply",
		"source_cross_repo_pull_request_create_preview",
		"source_cross_repo_pull_request_create",
		"source_public_pull_request_status",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names=%#v want=%#v", names, want)
	}

	calls := []struct {
		name string
		args string
		want string
	}{
		{"source_public_issue_status", `{"upstream_owner":"up","repo":"demo","number":9}`, "issue-status"},
		{"source_public_fork_create_preview", `{"upstream_owner":"up","repo":"demo"}`, "fork-preview"},
		{"source_public_fork_create", `{"plan_id":"f1","approve":true}`, "fork-create"},
		{"source_public_issue_comment_preview", `{"upstream_owner":"up","repo":"demo","number":9,"comment":"claim"}`, "comment-preview"},
		{"source_public_issue_comment", `{"plan_id":"c1","approve":true}`, "comment-create"},
		{"source_public_review_reply_preview", `{"upstream_owner":"up","repo":"demo","number":17,"comment_id":55,"reply":"done"}`, "review-reply-preview"},
		{"source_public_review_reply", `{"plan_id":"r1","approve":true}`, "review-reply"},
		{"source_cross_repo_pull_request_create_preview", `{"upstream_owner":"up","repo":"demo","head":"fix/x","base":"main","title":"Fix","description":"body","draft":false}`, "pr-preview"},
		{"source_cross_repo_pull_request_create", `{"plan_id":"p1","approve":true}`, "pr-create"},
		{"source_public_pull_request_status", `{"upstream_owner":"up","repo":"demo","number":17}`, "pr-status"},
	}
	for _, tc := range calls {
		got, err := byName[tc.name].Handler(json.RawMessage(tc.args))
		if err != nil || got != tc.want {
			t.Fatalf("%s got=%q err=%v", tc.name, got, err)
		}
	}
}
