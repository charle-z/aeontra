package development

import "testing"

func TestEnvironmentRecordRoundTripRejectsForgedCapabilityAndGeneration(t *testing.T) {
	capabilities, err := NewCapabilitySet("exec.argv", "toolchain.go")
	if err != nil {
		t.Fatal(err)
	}
	environment, err := NewEnvironmentAttestation("workcell:workspace-1", ClassWorkcell, 2, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	record, err := environment.Record()
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := record.Attestation()
	if err != nil || recovered.Digest != environment.Digest {
		t.Fatal("attestation transport did not round trip")
	}
	for _, mutate := range []func(*EnvironmentRecord){
		func(r *EnvironmentRecord) { r.Generation++ },
		func(r *EnvironmentRecord) { r.EnvironmentID = "other-project" },
		func(r *EnvironmentRecord) { r.Class = ClassIsolatedRunner },
		func(r *EnvironmentRecord) { r.Capabilities = append(r.Capabilities, "vm.root") },
		func(r *EnvironmentRecord) { r.Capabilities = []CapabilityID{"toolchain.go", "exec.argv"} },
	} {
		forged := record
		forged.Capabilities = append([]CapabilityID(nil), record.Capabilities...)
		mutate(&forged)
		if _, err := forged.Attestation(); err == nil {
			t.Fatalf("forged attestation accepted: %+v", forged)
		}
	}
}
