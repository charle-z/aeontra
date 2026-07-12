package mcpserver

import "testing"

func TestCatalogHashChangesWithSchemaAndAnnotations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Server)
	}{
		{
			name: "schema",
			mutate: func(s *Server) {
				entry := s.table["repo_list"]
				entry.def.InputSchema["additionalProperties"] = false
				s.table["repo_list"] = entry
			},
		},
		{
			name: "annotations",
			mutate: func(s *Server) {
				entry := s.table["repo_list"]
				entry.def.Annotations = map[string]any{
					"readOnlyHint":    false,
					"destructiveHint": false,
					"idempotentHint":  true,
					"openWorldHint":   false,
				}
				s.table["repo_list"] = entry
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := stampServer(t)
			baseline, err := s.CatalogInfo()
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(s)
			changed, err := s.CatalogInfo()
			if err != nil {
				t.Fatal(err)
			}
			if changed.Hash == baseline.Hash {
				t.Fatalf("catalog hash did not change after %s change", test.name)
			}
		})
	}
}

func TestCatalogSnapshotDoesNotExposeMutableRegistryMaps(t *testing.T) {
	s := stampServer(t)
	baseline, err := s.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}
	baseline.Tools[0].Annotations["readOnlyHint"] = "tampered"
	baseline.Tools[0].InputSchema["type"] = "tampered"

	after, err := s.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}
	if after.Hash != baseline.Hash {
		// baseline.Hash was computed before mutating the returned copy.
		t.Fatalf("mutating a returned snapshot changed the registry hash: %q != %q", after.Hash, baseline.Hash)
	}
}
