package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakeResultService struct {
	called string
}

func (service *fakeResultService) ResultRead(ref string, offset int64, maxBytes int) (string, error) {
	service.called = "read"
	return `{"result_ref":"` + ref + `"}`, nil
}

func (service *fakeResultService) ResultFind(query string, limit int) (string, error) {
	service.called = "find"
	return `[]`, nil
}

func (service *fakeResultService) ResultStage(ref string, stage int, maxBytes int) (string, error) {
	service.called = "stage"
	return `{"result_ref":"` + ref + `"}`, nil
}

func TestRegisterResultsDefinesClosedBoundedContractsAndRoutesHandlers(t *testing.T) {
	service := &fakeResultService{}
	var tools []Tool
	RegisterResults(func(tool Tool) { tools = append(tools, tool) }, service)
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
		if tool.InputSchema["additionalProperties"] != false {
			t.Fatalf("%s schema is not closed: %#v", tool.Name, tool.InputSchema)
		}
	}
	if !reflect.DeepEqual(names, []string{"result_read", "result_find", "result_stage"}) {
		t.Fatalf("names=%v", names)
	}

	ref := "rs_0123456789abcdef0123456789abcdef"
	arguments := []json.RawMessage{
		json.RawMessage(`{"result_ref":"` + ref + `","offset":0,"max_bytes":512}`),
		json.RawMessage(`{"query":"needle","limit":5}`),
		json.RawMessage(`{"result_ref":"` + ref + `","stage":0,"max_bytes":512}`),
	}
	for index, tool := range tools {
		if _, err := tool.Handler(arguments[index]); err != nil {
			t.Fatalf("%s handler: %v", tool.Name, err)
		}
	}
	if service.called != "stage" {
		t.Fatalf("last route=%q", service.called)
	}
	if _, err := tools[0].Handler(json.RawMessage(`{"result_ref":"` + ref + `","unknown":true}`)); err == nil {
		t.Fatal("unknown result_read argument accepted")
	}
}
