package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakeNotesService struct {
	calls []string
}

func (f *fakeNotesService) NotesList() (string, error) {
	f.calls = append(f.calls, "list")
	return "list-result", nil
}

func (f *fakeNotesService) NotesRead(name string) (string, error) {
	f.calls = append(f.calls, "read:"+name)
	return "read-result", nil
}

func (f *fakeNotesService) NotesWritePreview(name, content, mode string) (string, error) {
	f.calls = append(f.calls, "preview:"+name+":"+content+":"+mode)
	return "preview-result", nil
}

func (f *fakeNotesService) NotesWrite(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "write:"+planID)
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "write-result", nil
}

func TestRegisterNotesDefinesStableOrderAndRoutesHandlers(t *testing.T) {
	service := &fakeNotesService{}
	var registered []Tool
	RegisterNotes(func(tool Tool) {
		registered = append(registered, tool)
	}, service)

	gotNames := make([]string, 0, len(registered))
	for _, tool := range registered {
		gotNames = append(gotNames, tool.Name)
		if tool.Version != "1" {
			t.Fatalf("tool %s version = %q", tool.Name, tool.Version)
		}
	}
	wantNames := []string{"notes_list", "notes_read", "notes_write_preview", "notes_write"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names = %#v, want %#v", gotNames, wantNames)
	}

	calls := []struct {
		name string
		args string
		want string
	}{
		{name: "notes_list", args: `{}`, want: "list-result"},
		{name: "notes_read", args: `{"name":"alpha"}`, want: "read-result"},
		{name: "notes_write_preview", args: `{"name":"alpha","content":"body","mode":"append"}`, want: "preview-result"},
		{name: "notes_write", args: `{"plan_id":"plan-1","approve":true}`, want: "write-result"},
	}
	byName := map[string]Tool{}
	for _, tool := range registered {
		byName[tool.Name] = tool
	}
	for _, call := range calls {
		result, err := byName[call.name].Handler(json.RawMessage(call.args))
		if err != nil {
			t.Fatalf("%s: %v", call.name, err)
		}
		if result != call.want {
			t.Fatalf("%s result = %q, want %q", call.name, result, call.want)
		}
	}

	wantCalls := []string{"list", "read:alpha", "preview:alpha:body:append", "write:plan-1", "approved"}
	if !reflect.DeepEqual(service.calls, wantCalls) {
		t.Fatalf("service calls = %#v, want %#v", service.calls, wantCalls)
	}
}

func TestRegisterNotesPreservesRequiredFields(t *testing.T) {
	var registered []Tool
	RegisterNotes(func(tool Tool) {
		registered = append(registered, tool)
	}, &fakeNotesService{})

	byName := map[string]Tool{}
	for _, tool := range registered {
		byName[tool.Name] = tool
	}
	wantRequired := map[string][]string{
		"notes_list":          nil,
		"notes_read":          {"name"},
		"notes_write_preview": {"name", "content", "mode"},
		"notes_write":         {"plan_id"},
	}
	for name, required := range wantRequired {
		got, _ := byName[name].InputSchema["required"].([]string)
		if !reflect.DeepEqual(got, required) {
			t.Fatalf("%s required = %#v, want %#v", name, got, required)
		}
	}
}
