package catalog

import "encoding/json"

// NotesService is the narrow domain contract required by the notes tool group.
// The catalog package does not depend on the concrete tools.Service implementation.
type NotesService interface {
	NotesList() (string, error)
	NotesRead(name string) (string, error)
	NotesWritePreview(name, content, mode string) (string, error)
	NotesWrite(planID string, approve bool) (string, error)
}

// RegisterNotes registers persistent note reads and their planned write workflow in
// the same order used by the legacy monolithic registry.
func RegisterNotes(register Register, service NotesService) {
	register(Tool{
		Name:        "notes_list",
		Description: "List persistent Markdown user notes stored under the workspace root's .agent-memory/notes directory. Returns only validated names, update times and sizes; symlinks and non-Markdown files are skipped.",
		InputSchema: object(map[string]any{}),
		Version:     "1",
		Handler: func(json.RawMessage) (string, error) {
			return service.NotesList()
		},
	})

	register(Tool{
		Name:        "notes_read",
		Description: "Read one persistent Markdown user note by validated lowercase slug. The path is jailed, symlinks are rejected and content-level secrets are redacted.",
		InputSchema: object(map[string]any{"name": strProp("validated note slug without .md")}, "name"),
		Version:     "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.NotesRead(params.Name)
		},
	})

	register(Tool{
		Name:        "notes_write_preview",
		Description: "Validate and redact Markdown content for a create-or-append note operation, enforce the note size limit and current target state, and return an exact expiring single-use plan. It never overwrites or writes during preview.",
		InputSchema: object(map[string]any{
			"name":    strProp("validated lowercase note slug without .md"),
			"content": strProp("Markdown note content; secrets are redacted before planning"),
			"mode":    strProp("create or append; create refuses existing notes"),
		}, "name", "content", "mode"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Name    string `json:"name"`
				Content string `json:"content"`
				Mode    string `json:"mode"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.NotesWritePreview(params.Name, params.Content, params.Mode)
		},
	})

	register(Tool{
		Name:        "notes_write",
		Description: "Execute one reviewed notes_write_preview plan. It creates without overwrite or appends only if the existing content hash is unchanged; plan is expiring and single-use and requires approval in ask mode.",
		InputSchema: object(map[string]any{
			"plan_id": strProp("plan id returned by notes_write_preview"),
			"approve": boolProp("execute the note plan when approval is required"),
		}, "plan_id"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.NotesWrite(params.PlanID, params.Approve)
		},
	})
}
