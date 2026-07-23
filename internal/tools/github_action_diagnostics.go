package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

const (
	githubDiagnosticMaxJobs        = 8
	githubDiagnosticMaxAnnotations = 200
	githubDiagnosticDefaultLines   = 160
	githubDiagnosticMaxLines       = 500
	githubDiagnosticLogReadBytes   = 8 << 20
	githubAnnotationMaxPages       = 10
	githubActionSelectorMaxBytes   = 256
	githubJobLogDefaultChunkBytes  = 256 << 10
)

var githubANSISequence = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

type githubCheckAnnotation struct {
	Path            string `json:"path"`
	StartLine       int    `json:"start_line"`
	EndLine         int    `json:"end_line"`
	AnnotationLevel string `json:"annotation_level"`
	Title           string `json:"title"`
	Message         string `json:"message"`
}

type resolvedGitHubActionsJob struct {
	Run githubActionsRun
	Job githubActionsJob
}

type githubJobLogChunk struct {
	Data       []byte
	Offset     int64
	NextOffset int64
	Complete   bool
}

func validGitHubActionSelector(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > githubActionSelectorMaxBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func latestGitHubActionsRuns(runs []githubActionsRun, sha string) ([]githubActionsRun, error) {
	latest := make(map[int64]githubActionsRun)
	for _, run := range runs {
		if run.HeadSHA != sha {
			continue
		}
		if run.ID <= 0 || run.WorkflowID <= 0 || run.RunAttempt < 1 || strings.TrimSpace(run.Name) == "" {
			return nil, errors.New("GitHub Actions run metadata was invalid")
		}
		current, ok := latest[run.WorkflowID]
		if !ok || run.RunAttempt > current.RunAttempt || (run.RunAttempt == current.RunAttempt && run.ID > current.ID) {
			latest[run.WorkflowID] = run
		}
	}
	result := make([]githubActionsRun, 0, len(latest))
	for _, run := range latest {
		result = append(result, run)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func failedGitHubActionsJob(job githubActionsJob) bool {
	return job.Status == "completed" && !terminalSuccess(job.Conclusion)
}

func (c *GitHubClient) resolveGitHubActionsJobs(ctx context.Context, repo, sha, workflowName, jobName string, failedOnly bool) ([]resolvedGitHubActionsJob, error) {
	runs, err := c.githubActionsRuns(ctx, repo, sha)
	if err != nil {
		return nil, err
	}
	latest, err := latestGitHubActionsRuns(runs, sha)
	if err != nil {
		return nil, err
	}
	result := make([]resolvedGitHubActionsJob, 0)
	for _, run := range latest {
		if workflowName != "" && run.Name != workflowName {
			continue
		}
		jobs, err := c.githubActionsJobs(ctx, repo, run.ID)
		if err != nil {
			return nil, err
		}
		for _, job := range jobs {
			if job.ID <= 0 || strings.TrimSpace(job.Name) == "" {
				return nil, errors.New("GitHub Actions job metadata was invalid")
			}
			if jobName != "" && job.Name != jobName {
				continue
			}
			if failedOnly && !failedGitHubActionsJob(job) {
				continue
			}
			result = append(result, resolvedGitHubActionsJob{Run: run, Job: job})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Run.Name != result[j].Run.Name {
			return result[i].Run.Name < result[j].Run.Name
		}
		return result[i].Job.Name < result[j].Job.Name
	})
	return result, nil
}

func (c *GitHubClient) githubCheckAnnotations(ctx context.Context, repo string, checkRunID int64) ([]githubCheckAnnotation, error) {
	result := make([]githubCheckAnnotation, 0)
	for page := 1; page <= githubAnnotationMaxPages; page++ {
		path := fmt.Sprintf("/repos/%s/%s/check-runs/%d/annotations?per_page=100&page=%d", url.PathEscape(c.owner), url.PathEscape(repo), checkRunID, page)
		status, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, githubAnnotationsResponseLimit)
		if err != nil {
			return nil, err
		}
		if status == http.StatusForbidden {
			return nil, errors.New("GitHub check annotations require Checks: Read")
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("GitHub check annotations -> HTTP %d", status)
		}
		var pageItems []githubCheckAnnotation
		if err := json.Unmarshal([]byte(body), &pageItems); err != nil {
			return nil, fmt.Errorf("decoding GitHub check annotations: %w", err)
		}
		result = append(result, pageItems...)
		if len(pageItems) < 100 {
			return result, nil
		}
	}
	return nil, errors.New("GitHub check annotation pagination exceeded safety limit")
}

func (c *GitHubClient) githubJobLogRedirect(ctx context.Context, repo string, jobID int64) (string, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/jobs/%d/logs", url.PathEscape(c.owner), url.PathEscape(repo), jobID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	do := c.doNoRedirect
	if do == nil {
		client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
		do = client.Do
	}
	response, err := do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden {
		return "", errors.New("GitHub job logs require Actions: Read")
	}
	if response.StatusCode != http.StatusFound && response.StatusCode != http.StatusTemporaryRedirect {
		return "", fmt.Errorf("GitHub job log redirect -> HTTP %d", response.StatusCode)
	}
	location := strings.TrimSpace(response.Header.Get("Location"))
	if location == "" {
		return "", errors.New("GitHub job log redirect was missing")
	}
	if err := c.validateGitHubSignedLogURL(location); err != nil {
		return "", err
	}
	return location, nil
}

func (c *GitHubClient) validateGitHubSignedLogURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("GitHub job log redirect was unsafe")
	}
	base, _ := url.Parse(c.baseURL)
	localTest := base != nil && base.Scheme == "http" && parsed.Scheme == "http" && parsed.Host == base.Host && isLoopbackHost(parsed.Hostname())
	if parsed.Scheme != "https" && !localTest {
		return errors.New("GitHub job log redirect was not HTTPS")
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && !localTest {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return errors.New("GitHub job log redirect targeted a private address")
		}
	}
	return nil
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func (c *GitHubClient) githubJobLogChunk(ctx context.Context, repo string, jobID int64, offset, maxBytes int64) (githubJobLogChunk, error) {
	if offset < 0 || maxBytes < 1 || maxBytes > githubJobLogTotalMaxBytes || offset > githubJobLogTotalMaxBytes || offset+maxBytes > githubJobLogTotalMaxBytes {
		return githubJobLogChunk{}, errors.New("GitHub job log range exceeded safety limit")
	}
	location, err := c.githubJobLogRedirect(ctx, repo, jobID)
	if err != nil {
		return githubJobLogChunk{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return githubJobLogChunk{}, err
	}
	req.Header.Set("Accept", "text/plain, application/octet-stream;q=0.9")
	req.Header.Set("Accept-Encoding", "identity")
	do := c.doSigned
	if do == nil {
		do = (&http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}).Do
	}
	response, err := do(req)
	if err != nil {
		return githubJobLogChunk{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return githubJobLogChunk{}, fmt.Errorf("GitHub signed job log -> HTTP %d", response.StatusCode)
	}
	if offset > 0 {
		copied, err := io.CopyN(io.Discard, response.Body, offset)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return githubJobLogChunk{Offset: offset, NextOffset: copied, Complete: true}, nil
		}
		if err != nil {
			return githubJobLogChunk{}, fmt.Errorf("skipping GitHub job log: %w", err)
		}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return githubJobLogChunk{}, fmt.Errorf("reading GitHub job log: %w", err)
	}
	complete := int64(len(data)) <= maxBytes
	if !complete {
		data = data[:maxBytes]
	}
	return githubJobLogChunk{Data: data, Offset: offset, NextOffset: offset + int64(len(data)), Complete: complete}, nil
}

func safeGitHubDiagnosticField(value string, maxBytes int) string {
	value = githubANSISequence.ReplaceAllString(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(value), "\\r", " "), "\\n", " "), "")
	if maxBytes > 0 && len(value) > maxBytes {
		value = value[:maxBytes] + "…"
	}
	return value
}

func cleanGitHubLogText(data []byte) string {
	text := strings.ReplaceAll(string(data), "\x00", "�")
	return githubANSISequence.ReplaceAllString(text, "")
}

func diagnosticGitHubLogLines(text string, maxLines int) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	markers := []string{"error", "fatal", "fail", "panic", "exit code", "exit status", "permission denied", "not found", "assert", "timed out"}
	selected := make(map[int]struct{})
	for i, line := range lines {
		lower := strings.ToLower(line)
		matched := false
		for _, marker := range markers {
			if strings.Contains(lower, marker) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		for j := i - 2; j <= i+2; j++ {
			if j >= 0 && j < len(lines) {
				selected[j] = struct{}{}
			}
		}
	}
	tail := maxLines / 3
	if tail < 20 {
		tail = 20
	}
	for i := len(lines) - tail; i < len(lines); i++ {
		if i >= 0 {
			selected[i] = struct{}{}
		}
	}
	indexes := make([]int, 0, len(selected))
	for index := range selected {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	if len(indexes) > maxLines {
		indexes = indexes[len(indexes)-maxLines:]
	}
	result := make([]string, 0, len(indexes))
	for _, index := range indexes {
		result = append(result, fmt.Sprintf("log_line %d: %s", index+1, lines[index]))
	}
	return result
}

func (s *SourceCapability) SourcePullRequestFailureDiagnostics(repo string, number int, workflowName, jobName string, maxLines int) (string, error) {
	span := s.log.Start("source_pull_request_failure_diagnostics")
	if err := s.github.configError(); err != nil {
		return "", err
	}
	if !safeCloneDir(strings.TrimSpace(repo)) || number < 1 {
		return "", errors.New("invalid repository or pull request number")
	}
	workflowName = strings.TrimSpace(workflowName)
	jobName = strings.TrimSpace(jobName)
	if workflowName != "" && !validGitHubActionSelector(workflowName) {
		return "", errors.New("invalid workflow name")
	}
	if jobName != "" && !validGitHubActionSelector(jobName) {
		return "", errors.New("invalid job name")
	}
	if maxLines == 0 {
		maxLines = githubDiagnosticDefaultLines
	}
	if maxLines < 20 || maxLines > githubDiagnosticMaxLines {
		return "", errors.New("diagnostic line limit must be between 20 and 500")
	}
	ctx := context.Background()
	pull, err := s.github.pullRequest(ctx, repo, number)
	if err != nil {
		return "", err
	}
	jobs, err := s.github.resolveGitHubActionsJobs(ctx, repo, pull.Head.SHA, workflowName, jobName, true)
	if err != nil {
		return "", err
	}
	if len(jobs) == 0 {
		return "", errors.New("no matching failed GitHub Actions job exists on the pull request head")
	}
	truncatedJobs := len(jobs) > githubDiagnosticMaxJobs
	if truncatedJobs {
		jobs = jobs[:githubDiagnosticMaxJobs]
	}
	var output strings.Builder
	fmt.Fprintf(&output, "pull_request: %d\nhead_sha: %s\nfailed_jobs: %d\n", pull.Number, pull.Head.SHA, len(jobs))
	for _, item := range jobs {
		fmt.Fprintf(&output, "\nworkflow: %s\nrun_attempt: %d\njob: %s\nconclusion: %s\nurl: %s\n", item.Run.Name, item.Run.RunAttempt, item.Job.Name, item.Job.Conclusion, item.Job.HTMLURL)
		for _, step := range item.Job.Steps {
			if step.Status == "completed" && !terminalSuccess(step.Conclusion) {
				fmt.Fprintf(&output, "failed_step: %d | %s | conclusion=%s\n", step.Number, step.Name, step.Conclusion)
			}
		}
		annotations, annotationErr := s.github.githubCheckAnnotations(ctx, repo, item.Job.ID)
		if annotationErr != nil {
			fmt.Fprintf(&output, "annotations_error: %s\n", annotationErr)
		} else {
			fmt.Fprintf(&output, "annotations: %d\n", len(annotations))
			shown := annotations
			if len(shown) > githubDiagnosticMaxAnnotations {
				shown = shown[:githubDiagnosticMaxAnnotations]
			}
			for _, annotation := range shown {
				line := annotation.StartLine
				if line < 0 {
					line = 0
				}
				fmt.Fprintf(&output, "annotation: %s:%d | level=%s | title=%s | message=%s\n",
					safeGitHubDiagnosticField(annotation.Path, 1024), line,
					safeGitHubDiagnosticField(annotation.AnnotationLevel, 64),
					safeGitHubDiagnosticField(annotation.Title, 1024),
					safeGitHubDiagnosticField(annotation.Message, 4096))
			}
			if len(annotations) > len(shown) {
				output.WriteString("annotations_truncated: true\n")
			}
		}
		chunk, logErr := s.github.githubJobLogChunk(ctx, repo, item.Job.ID, 0, githubDiagnosticLogReadBytes)
		if logErr != nil {
			fmt.Fprintf(&output, "log_error: %s\n", logErr)
			continue
		}
		fmt.Fprintf(&output, "log_complete_within_%d_bytes: %t\n", githubDiagnosticLogReadBytes, chunk.Complete)
		for _, line := range diagnosticGitHubLogLines(cleanGitHubLogText(chunk.Data), maxLines) {
			output.WriteString(line + "\n")
		}
	}
	if truncatedJobs {
		output.WriteString("failed_jobs_truncated: true\n")
	}
	span.Finish(audit.Allow, repo+" #"+strconv.Itoa(number), nil, nil)
	return s.redact(output.String()), nil
}

func (s *SourceCapability) SourcePullRequestJobLog(repo string, number int, workflowName, jobName string, offsetBytes, maxBytes int) (string, error) {
	span := s.log.Start("source_pull_request_job_log")
	if err := s.github.configError(); err != nil {
		return "", err
	}
	if !safeCloneDir(strings.TrimSpace(repo)) || number < 1 || !validGitHubActionSelector(strings.TrimSpace(jobName)) {
		return "", errors.New("invalid repository, pull request number or job name")
	}
	workflowName = strings.TrimSpace(workflowName)
	jobName = strings.TrimSpace(jobName)
	if workflowName != "" && !validGitHubActionSelector(workflowName) {
		return "", errors.New("invalid workflow name")
	}
	if maxBytes == 0 {
		maxBytes = githubJobLogDefaultChunkBytes
	}
	if maxBytes < 1024 || maxBytes > githubJobLogChunkMaxBytes || offsetBytes < 0 || offsetBytes > githubJobLogTotalMaxBytes || offsetBytes+maxBytes > githubJobLogTotalMaxBytes {
		return "", errors.New("job log range is outside the allowed 16 MiB window")
	}
	ctx := context.Background()
	pull, err := s.github.pullRequest(ctx, repo, number)
	if err != nil {
		return "", err
	}
	jobs, err := s.github.resolveGitHubActionsJobs(ctx, repo, pull.Head.SHA, workflowName, jobName, false)
	if err != nil {
		return "", err
	}
	if len(jobs) == 0 {
		return "", errors.New("matching GitHub Actions job was not found on the pull request head")
	}
	if len(jobs) > 1 {
		workflows := make([]string, 0, len(jobs))
		for _, item := range jobs {
			workflows = append(workflows, item.Run.Name)
		}
		sort.Strings(workflows)
		return "", fmt.Errorf("job name is ambiguous; specify workflow_name from: %s", strings.Join(workflows, ", "))
	}
	item := jobs[0]
	chunk, err := s.github.githubJobLogChunk(ctx, repo, item.Job.ID, int64(offsetBytes), int64(maxBytes))
	if err != nil {
		return "", err
	}
	var output strings.Builder
	fmt.Fprintf(&output, "pull_request: %d\nhead_sha: %s\nworkflow: %s\nrun_attempt: %d\njob: %s\nconclusion: %s\noffset_bytes: %d\nreturned_bytes: %d\nnext_offset: %d\ncomplete: %t\n--- BEGIN REDACTED JOB LOG ---\n", pull.Number, pull.Head.SHA, item.Run.Name, item.Run.RunAttempt, item.Job.Name, item.Job.Conclusion, chunk.Offset, len(chunk.Data), chunk.NextOffset, chunk.Complete)
	output.WriteString(cleanGitHubLogText(chunk.Data))
	if len(chunk.Data) > 0 && chunk.Data[len(chunk.Data)-1] != '\n' {
		output.WriteByte('\n')
	}
	output.WriteString("--- END REDACTED JOB LOG ---\n")
	span.Finish(audit.Allow, repo+" #"+strconv.Itoa(number)+" "+item.Job.Name, nil, nil)
	return s.redact(output.String()), nil
}
