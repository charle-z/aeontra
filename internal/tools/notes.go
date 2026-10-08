package tools

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

const (
	notesDir     = ".agent-memory/notes"
	maxNoteBytes = 64 << 10
)

var noteSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func (s *RepositoryCapability) NotesList() (string, error) {
	sp := s.log.Start("notes_list")
	base, err := s.pol.CheckRead(filepath.Join(s.root, filepath.FromSlash(notesDir)))
	if err != nil {
		sp.Finish(audit.Deny, "list", nil, err)
		return "", err
	}
	entries, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		sp.Finish(audit.Allow, "list", nil, nil)
		return "notes: []\n", nil
	}
	if err != nil {
		sp.Finish(audit.Error, "list", nil, err)
		return "", err
	}
	type noteInfo struct {
		name    string
		updated time.Time
		size    int64
	}
	var notes []noteInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			continue
		}
		slug := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if !noteSlugRe.MatchString(slug) {
			continue
		}
		path := filepath.Join(base, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		notes = append(notes, noteInfo{name: slug, updated: info.ModTime().UTC(), size: info.Size()})
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].name < notes[j].name })
	var b strings.Builder
	if len(notes) == 0 {
		b.WriteString("notes: []\n")
	}
	for _, note := range notes {
		fmt.Fprintf(&b, "name: %s\nupdated: %s\nsize: %d\n\n", note.name, note.updated.Format(time.RFC3339), note.size)
	}
	sp.Finish(audit.Allow, "list", nil, nil)
	return b.String(), nil
}

func (s *RepositoryCapability) NotesRead(name string) (string, error) {
	sp := s.log.Start("notes_read")
	target, err := s.noteTarget(name)
	if err != nil {
		sp.Finish(audit.Deny, summarize(name), nil, err)
		return "", err
	}
	info, err := os.Lstat(target)
	if err != nil {
		sp.Finish(audit.Error, name, []string{target}, err)
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		err := fmt.Errorf("note must be a regular Markdown file, not a symlink")
		sp.Finish(audit.Deny, name, []string{target}, err)
		return "", err
	}
	content, err := readContained(s.root, target)
	if err != nil {
		sp.Finish(audit.Error, name, []string{target}, err)
		return "", err
	}
	sp.Finish(audit.Allow, name, []string{target}, nil)
	return s.redact(content), nil
}

func (s *RepositoryCapability) NotesWritePreview(name, content, mode string) (string, error) {
	sp := s.log.Start("notes_write_preview")
	name = strings.TrimSpace(name)
	target, err := s.noteTarget(name)
	if err != nil {
		sp.Finish(audit.Deny, summarize(name), nil, err)
		return "", err
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "create" && mode != "append" {
		err := fmt.Errorf("note mode must be create or append")
		sp.Finish(audit.Deny, name, []string{target}, err)
		return "", err
	}
	if strings.TrimSpace(content) == "" {
		err := fmt.Errorf("note content is required")
		sp.Finish(audit.Deny, name, []string{target}, err)
		return "", err
	}
	info, statErr := os.Lstat(target)
	exists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		sp.Finish(audit.Error, name, []string{target}, statErr)
		return "", statErr
	}
	if exists && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		err := fmt.Errorf("note target must be a regular Markdown file, not a symlink")
		sp.Finish(audit.Deny, name, []string{target}, err)
		return "", err
	}
	if mode == "create" && exists {
		err := fmt.Errorf("note %q already exists; overwrite is refused (use append)", name)
		sp.Finish(audit.Deny, name, []string{target}, err)
		return "", err
	}
	if mode == "append" && !exists {
		err := fmt.Errorf("note %q does not exist; create it first", name)
		sp.Finish(audit.Deny, name, []string{target}, err)
		return "", err
	}
	safe := strings.TrimSpace(s.redact(content))
	body := safe + "\n"
	currentHash := ""
	currentSize := int64(0)
	if mode == "append" {
		current, err := os.ReadFile(target)
		if err != nil {
			sp.Finish(audit.Error, name, []string{target}, err)
			return "", err
		}
		currentSize = int64(len(current))
		currentHash = fmt.Sprintf("%x", sha256.Sum256(current))
		stamp := time.Now().UTC().Format(time.RFC3339)
		body = fmt.Sprintf("\n\n<!-- appended: %s -->\n\n%s\n", stamp, safe)
	}
	resultSize := currentSize + int64(len(body))
	if resultSize > maxNoteBytes {
		err := fmt.Errorf("note would exceed %d byte size limit", maxNoteBytes)
		sp.Finish(audit.Deny, name, []string{target}, err)
		return "", err
	}
	plan, err := s.plans.Create("notes-write", map[string]string{
		"name": name, "target": target, "mode": mode, "body": body,
		"exists": fmt.Sprintf("%t", exists), "current_hash": currentHash,
		"result_size": fmt.Sprintf("%d", resultSize),
	})
	if err != nil {
		sp.Finish(audit.Error, name, []string{target}, err)
		return "", err
	}
	sp.Finish(audit.Allow, name+" "+plan.ID, []string{target}, nil)
	return fmt.Sprintf("target: %s\nmode: %s\nresulting_size_estimate: %d\nplan_id: %s\nexpiry: %s\n",
		filepath.ToSlash(filepath.Join(notesDir, name+".md")), mode, resultSize, plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *RepositoryCapability) NotesWrite(planID string, approve bool) (string, error) {
	sp := s.log.Start("notes_write")
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: notes_write would execute the reviewed single-use note plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "notes-write")
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	resolved, _, err := s.pol.CheckWrite(plan.Args["target"])
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if resolved != plan.Args["target"] {
		err := fmt.Errorf("note target changed after preview")
		sp.Finish(audit.Deny, planID, []string{resolved}, err)
		return "", err
	}
	info, statErr := os.Lstat(resolved)
	exists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		sp.Finish(audit.Error, planID, []string{resolved}, statErr)
		return "", statErr
	}
	if exists && info.Mode()&os.ModeSymlink != 0 {
		err := fmt.Errorf("note target changed into a symlink")
		sp.Finish(audit.Deny, planID, []string{resolved}, err)
		return "", err
	}
	if plan.Args["mode"] == "create" && exists {
		err := fmt.Errorf("note target changed after preview and now exists")
		sp.Finish(audit.Deny, planID, []string{resolved}, err)
		return "", err
	}
	if plan.Args["mode"] == "append" {
		if !exists {
			err := fmt.Errorf("note target changed after preview and no longer exists")
			sp.Finish(audit.Deny, planID, []string{resolved}, err)
			return "", err
		}
		current, err := os.ReadFile(resolved)
		if err != nil {
			sp.Finish(audit.Error, planID, []string{resolved}, err)
			return "", err
		}
		if fmt.Sprintf("%x", sha256.Sum256(current)) != plan.Args["current_hash"] {
			err := fmt.Errorf("note content changed after preview")
			sp.Finish(audit.Deny, planID, []string{resolved}, err)
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		sp.Finish(audit.Error, planID, []string{resolved}, err)
		return "", err
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if plan.Args["mode"] == "append" {
		flags = os.O_WRONLY | os.O_APPEND
	}
	f, err := os.OpenFile(resolved, flags, 0o644)
	if err != nil {
		sp.Finish(audit.Error, planID, []string{resolved}, err)
		return "", err
	}
	_, writeErr := f.WriteString(plan.Args["body"])
	closeErr := f.Close()
	if writeErr != nil {
		err = writeErr
	} else {
		err = closeErr
	}
	if err != nil {
		sp.Finish(audit.Error, planID, []string{resolved}, err)
		return "", err
	}
	sp.Finish(audit.Allow, planID, []string{resolved}, nil)
	return fmt.Sprintf("note %s: %s", plan.Args["mode"], filepath.ToSlash(filepath.Join(notesDir, plan.Args["name"]+".md"))), nil
}

func (s *RepositoryCapability) noteTarget(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !noteSlugRe.MatchString(name) {
		return "", fmt.Errorf("invalid note name %q (use lowercase slug characters a-z, 0-9, _ or -)", name)
	}
	target := filepath.Join(s.root, filepath.FromSlash(notesDir), name+".md")
	resolved, err := s.pol.CheckRead(target)
	if err != nil {
		return "", err
	}
	return resolved, nil
}
