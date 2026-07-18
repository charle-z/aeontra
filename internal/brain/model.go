// Package brain implements the server-anchored cross-repository memory contract.
// Markdown files are the source of truth; derived indexes are disposable.
package brain

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charle-z/mcp-devbox/internal/policy"
	"go.yaml.in/yaml/v3"
)

const (
	MaxSlugBytes           = 64
	MaxTitleBytes          = 160
	MaxConsoleSummaryBytes = 160
	MaxProvenanceBytes     = 1024
	MaxBodyBytes           = 32 << 10
	MaxFileBytes           = 40 << 10
	MaxAgentNameBytes      = 64

	CuratedDir = "curated"
	WorkingDir = "working"
	CacheDir   = ".cache"

	AuthorOwner = "owner"
)

// TrustLevel is the source directory's authority level.
type TrustLevel string

const (
	TrustCurated TrustLevel = "curated"
	TrustWorking TrustLevel = "working"
)

// NoteType is the explicit semantic posture of a note.
type NoteType string

const (
	TypeFact       NoteType = "fact"
	TypeNote       NoteType = "note"
	TypeFeedback   NoteType = "feedback"
	TypeReference  NoteType = "reference"
	TypeHypothesis NoteType = "hypothesis"
)

var (
	slugPattern   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	agentPattern  = regexp.MustCompile(`^agent:[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?$`)
	typeSet       = map[NoteType]bool{TypeFact: true, TypeNote: true, TypeFeedback: true, TypeReference: true, TypeHypothesis: true}
	trustSet      = map[TrustLevel]bool{TrustCurated: true, TrustWorking: true}
	ErrCuratedRO  = errors.New("brain: curated notes are owner-only and read-only to agents")
	ErrSecretData = errors.New("brain: note contains secret-like material")
)

// Metadata is the exact YAML frontmatter schema. Unknown fields are rejected.
type Metadata struct {
	Slug           string   `yaml:"slug" json:"slug"`
	Title          string   `yaml:"title" json:"title"`
	Type           NoteType `yaml:"type" json:"type"`
	Author         string   `yaml:"author" json:"author"`
	Created        string   `yaml:"created" json:"created"`
	Updated        string   `yaml:"updated" json:"updated"`
	Provenance     string   `yaml:"provenance" json:"provenance"`
	ReviewBy       string   `yaml:"review_by,omitempty" json:"review_by,omitempty"`
	ConsoleSummary string   `yaml:"console_summary,omitempty" json:"console_summary,omitempty"`
}

// Note is one validated source note plus derived trust/link state.
type Note struct {
	Metadata Metadata   `json:"metadata"`
	Body     string     `json:"body"`
	Trust    TrustLevel `json:"trust"`
	Links    []string   `json:"links"`
	Expired  bool       `json:"expired"`
}

// ReadResult is one validated note plus bounded backlinks.
type ReadResult struct {
	Note      Note     `json:"note"`
	Backlinks []string `json:"backlinks"`
}

// AgentDraft is the bounded public input used to construct a working note. Clients
// cannot supply timestamps, a source path, or a trust directory.
type AgentDraft struct {
	Slug       string
	Title      string
	Type       NoteType
	Author     string
	Provenance string
	ReviewBy   string
	Body       string
}

// ValidateSlug enforces strict ASCII kebab-case without traversal/path syntax.
func ValidateSlug(slug string) error {
	if slug == "" || len(slug) > MaxSlugBytes || !utf8.ValidString(slug) || !slugPattern.MatchString(slug) {
		return fmt.Errorf("brain: invalid slug (use 1-%d bytes of strict lowercase kebab-case)", MaxSlugBytes)
	}
	return nil
}

func validateTrust(trust TrustLevel) error {
	if !trustSet[trust] {
		return errors.New("brain: invalid trust level")
	}
	return nil
}

func validateType(value NoteType) error {
	if !typeSet[value] {
		return errors.New("brain: invalid note type")
	}
	return nil
}

func validateAuthor(author string, trust TrustLevel) error {
	if author == AuthorOwner {
		if trust == TrustCurated || trust == TrustWorking {
			return nil
		}
	}
	if !agentPattern.MatchString(author) || len(strings.TrimPrefix(author, "agent:")) > MaxAgentNameBytes {
		return fmt.Errorf("brain: invalid author (use owner or agent:<validated-name>)")
	}
	if trust == TrustCurated {
		return ErrCuratedRO
	}
	return nil
}

func validateSingleLine(name, value string, maximum int) error {
	if value == "" || value != strings.TrimSpace(value) || len(value) > maximum || strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("brain: %s must be a trimmed single line of 1-%d bytes", name, maximum)
	}
	return nil
}

func parseUTCRFC3339(name, value string) (time.Time, error) {
	if !strings.HasSuffix(value, "Z") {
		return time.Time{}, fmt.Errorf("brain: %s must be UTC RFC3339", name)
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Format(time.RFC3339Nano) != value {
		return time.Time{}, fmt.Errorf("brain: %s must be canonical UTC RFC3339", name)
	}
	return parsed.UTC(), nil
}

func parseReviewDate(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return time.Time{}, fmt.Errorf("brain: review_by must use YYYY-MM-DD")
	}
	return parsed.UTC(), nil
}

func dateOnly(value time.Time) time.Time {
	year, month, day := value.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func validateMetadata(metadata Metadata, expectedSlug string, trust TrustLevel, now time.Time, agentWrite bool) (expired bool, err error) {
	if err := validateTrust(trust); err != nil {
		return false, err
	}
	if err := ValidateSlug(metadata.Slug); err != nil {
		return false, err
	}
	if expectedSlug != "" && metadata.Slug != expectedSlug {
		return false, fmt.Errorf("brain: filename/frontmatter slug mismatch")
	}
	if err := validateSingleLine("title", metadata.Title, MaxTitleBytes); err != nil {
		return false, err
	}
	if metadata.ConsoleSummary != "" {
		if err := validateSingleLine("console_summary", metadata.ConsoleSummary, MaxConsoleSummaryBytes); err != nil {
			return false, err
		}
	}
	if err := validateType(metadata.Type); err != nil {
		return false, err
	}
	if err := validateAuthor(metadata.Author, trust); err != nil {
		return false, err
	}
	if agentWrite && !strings.HasPrefix(metadata.Author, "agent:") {
		return false, fmt.Errorf("brain: agent writes require author agent:<name>")
	}
	if err := validateSingleLine("provenance", metadata.Provenance, MaxProvenanceBytes); err != nil {
		return false, err
	}
	created, err := parseUTCRFC3339("created", metadata.Created)
	if err != nil {
		return false, err
	}
	updated, err := parseUTCRFC3339("updated", metadata.Updated)
	if err != nil {
		return false, err
	}
	if created.After(updated) {
		return false, fmt.Errorf("brain: created must not be after updated")
	}
	if updated.After(now.UTC().Add(5 * time.Minute)) {
		return false, fmt.Errorf("brain: updated timestamp is in the future")
	}
	if strings.HasPrefix(metadata.Author, "agent:") && metadata.ReviewBy == "" {
		return false, fmt.Errorf("brain: agent notes require review_by")
	}
	if metadata.ReviewBy != "" {
		review, err := parseReviewDate(metadata.ReviewBy)
		if err != nil {
			return false, err
		}
		today := dateOnly(now)
		expired = review.Before(today)
		if agentWrite {
			if expired {
				return false, fmt.Errorf("brain: review_by must not be in the past")
			}
			if review.After(today.AddDate(1, 0, 0)) {
				return false, fmt.Errorf("brain: review_by must be within 365 days")
			}
		}
	}
	return expired, nil
}

// ParseNote validates one strict frontmatter + Markdown source file.
func ParseNote(data []byte, expectedSlug string, trust TrustLevel, now time.Time) (Note, error) {
	if len(data) == 0 || len(data) > MaxFileBytes {
		return Note{}, fmt.Errorf("brain: source file must be 1-%d bytes", MaxFileBytes)
	}
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return Note{}, fmt.Errorf("brain: YAML frontmatter opening delimiter is required")
	}
	rest := normalized[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return Note{}, fmt.Errorf("brain: YAML frontmatter closing delimiter is required")
	}
	header := rest[:end]
	body := strings.TrimPrefix(rest[end+len("\n---\n"):], "\n")
	body = strings.TrimSpace(body)
	if body == "" || len(body) > MaxBodyBytes {
		return Note{}, fmt.Errorf("brain: body must be 1-%d bytes", MaxBodyBytes)
	}

	decoder := yaml.NewDecoder(strings.NewReader(header))
	decoder.KnownFields(true)
	var metadata Metadata
	if err := decoder.Decode(&metadata); err != nil {
		return Note{}, fmt.Errorf("brain: invalid frontmatter: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Note{}, fmt.Errorf("brain: frontmatter must contain one YAML document")
	}
	expired, err := validateMetadata(metadata, expectedSlug, trust, now, false)
	if err != nil {
		return Note{}, err
	}
	links, err := ExtractLinks(body)
	if err != nil {
		return Note{}, err
	}
	return Note{Metadata: metadata, Body: body, Trust: trust, Links: links, Expired: expired}, nil
}

// ExtractLinks validates and returns sorted unique [[slug]] links.
func ExtractLinks(body string) ([]string, error) {
	seen := map[string]bool{}
	for offset := 0; ; {
		startRelative := strings.Index(body[offset:], "[[")
		if startRelative < 0 {
			break
		}
		start := offset + startRelative
		endRelative := strings.Index(body[start+2:], "]]")
		if endRelative < 0 {
			return nil, fmt.Errorf("brain: unclosed note link")
		}
		end := start + 2 + endRelative
		target := body[start+2 : end]
		if strings.Contains(target, "[[") || strings.Contains(target, "]]") || ValidateSlug(target) != nil {
			return nil, errors.New("brain: invalid note link")
		}
		seen[target] = true
		offset = end + 2
	}
	links := make([]string, 0, len(seen))
	for link := range seen {
		links = append(links, link)
	}
	sort.Strings(links)
	return links, nil
}

// BuildAgentNote constructs a validated working note with server-owned timestamps.
func BuildAgentNote(draft AgentDraft, existing *Note, now time.Time) (Note, error) {
	now = now.UTC()
	if draft.Author == AuthorOwner {
		return Note{}, fmt.Errorf("brain: agent writes cannot claim owner authority")
	}
	created := now.Format(time.RFC3339)
	if existing != nil {
		if existing.Trust != TrustWorking || existing.Metadata.Slug != draft.Slug {
			return Note{}, fmt.Errorf("brain: existing note does not match working target")
		}
		if existing.Metadata.Author != draft.Author {
			return Note{}, fmt.Errorf("brain: working note author is immutable")
		}
		created = existing.Metadata.Created
	}
	metadata := Metadata{
		Slug:       strings.TrimSpace(draft.Slug),
		Title:      strings.TrimSpace(draft.Title),
		Type:       draft.Type,
		Author:     strings.TrimSpace(draft.Author),
		Created:    created,
		Updated:    now.Format(time.RFC3339),
		Provenance: strings.TrimSpace(draft.Provenance),
		ReviewBy:   strings.TrimSpace(draft.ReviewBy),
	}
	if existing != nil {
		metadata.ConsoleSummary = existing.Metadata.ConsoleSummary
	}
	body := strings.TrimSpace(draft.Body)
	if body == "" || len(body) > MaxBodyBytes {
		return Note{}, fmt.Errorf("brain: body must be 1-%d bytes", MaxBodyBytes)
	}
	if _, changed := policy.Redact(strings.Join([]string{metadata.Title, metadata.Provenance, body}, "\n")); changed {
		return Note{}, ErrSecretData
	}
	expired, err := validateMetadata(metadata, metadata.Slug, TrustWorking, now, true)
	if err != nil {
		return Note{}, err
	}
	links, err := ExtractLinks(body)
	if err != nil {
		return Note{}, err
	}
	note := Note{Metadata: metadata, Body: body, Trust: TrustWorking, Links: links, Expired: expired}
	if len(RenderNote(note)) > MaxFileBytes {
		return Note{}, fmt.Errorf("brain: rendered source exceeds %d bytes", MaxFileBytes)
	}
	return note, nil
}

// RenderNote returns deterministic strict frontmatter and Markdown.
func RenderNote(note Note) []byte {
	header, err := yaml.Marshal(note.Metadata)
	if err != nil {
		panic(fmt.Sprintf("brain: validated metadata failed to render: %v", err))
	}
	var output bytes.Buffer
	output.WriteString("---\n")
	output.Write(header)
	output.WriteString("---\n\n")
	output.WriteString(strings.TrimSpace(note.Body))
	output.WriteByte('\n')
	return output.Bytes()
}
