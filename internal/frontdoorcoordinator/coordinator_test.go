package frontdoorcoordinator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNextCutoverPhaseFromManagedTopology(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		front        string
		frontBackend string
		backend      string
		phase        Phase
		done         bool
	}{
		{name: "add alternate backend", front: FrontTemporaryOrigin, frontBackend: FrontPublicOrigin, backend: FrontPublicOrigin, phase: PhaseAddBackendOrigin},
		{name: "switch facade backend", front: FrontTemporaryOrigin, frontBackend: FrontPublicOrigin, backend: FrontPublicOrigin + "," + BackendOrigin, phase: PhaseSwitchFrontBackend},
		{name: "release public backend", front: FrontTemporaryOrigin, frontBackend: BackendOrigin, backend: FrontPublicOrigin + "," + BackendOrigin, phase: PhaseReleasePublicBackend},
		{name: "assign public facade", front: FrontTemporaryOrigin, frontBackend: BackendOrigin, backend: BackendOrigin, phase: PhaseAssignPublicFront},
		{name: "closed", front: FrontPublicOrigin, frontBackend: BackendOrigin, backend: BackendOrigin, phase: PhaseComplete, done: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			phase, done, err := NextPhase(TargetCutover, Topology{FrontDomain: tc.front, FrontBackendURL: tc.frontBackend, BackendDomains: tc.backend})
			if err != nil {
				t.Fatal(err)
			}
			if phase != tc.phase || done != tc.done {
				t.Fatalf("phase=%q done=%t, want phase=%q done=%t", phase, done, tc.phase, tc.done)
			}
		})
	}
}

func TestNextRollbackPhaseFromManagedTopology(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		front        string
		frontBackend string
		backend      string
		phase        Phase
		done         bool
	}{
		{name: "move facade temporary", front: FrontPublicOrigin, frontBackend: BackendOrigin, backend: BackendOrigin, phase: PhaseMoveFrontTemporary},
		{name: "restore public backend", front: FrontTemporaryOrigin, frontBackend: BackendOrigin, backend: BackendOrigin, phase: PhaseRestorePublicBackend},
		{name: "switch facade to public backend", front: FrontTemporaryOrigin, frontBackend: BackendOrigin, backend: FrontPublicOrigin + "," + BackendOrigin, phase: PhaseSwitchFrontPublicBackend},
		{name: "remove alternate backend", front: FrontTemporaryOrigin, frontBackend: FrontPublicOrigin, backend: FrontPublicOrigin + "," + BackendOrigin, phase: PhaseRemoveAlternateBackend},
		{name: "rolled back", front: FrontTemporaryOrigin, frontBackend: FrontPublicOrigin, backend: FrontPublicOrigin, phase: PhaseComplete, done: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			phase, done, err := NextPhase(TargetRollback, Topology{FrontDomain: tc.front, FrontBackendURL: tc.frontBackend, BackendDomains: tc.backend})
			if err != nil {
				t.Fatal(err)
			}
			if phase != tc.phase || done != tc.done {
				t.Fatalf("phase=%q done=%t, want phase=%q done=%t", phase, done, tc.phase, tc.done)
			}
		})
	}
}

func TestNextPhaseRejectsUnknownTopology(t *testing.T) {
	t.Parallel()
	if _, _, err := NextPhase(TargetCutover, Topology{FrontDomain: "https://other.example", FrontBackendURL: BackendOrigin, BackendDomains: BackendOrigin}); err == nil {
		t.Fatal("unknown topology was accepted")
	}
}

func TestJournalIsAtomicAndMonotonic(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := OpenJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Advance(Status{RequestID: testRequestID, Target: TargetCutover, State: StateRunning, Phase: PhaseAddBackendOrigin})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Advance(Status{RequestID: testRequestID, Target: TargetCutover, State: StateRunning, Phase: PhaseSwitchFrontBackend})
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 || second.Revision != 2 {
		t.Fatalf("revisions = %d,%d", first.Revision, second.Revision)
	}
	loaded, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Revision != second.Revision || loaded.Phase != second.Phase {
		t.Fatalf("loaded=%+v second=%+v", loaded, second)
	}
	if _, err := os.Stat(filepath.Join(root, JournalFilename)); err != nil {
		t.Fatal(err)
	}
}
