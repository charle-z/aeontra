package development

import "testing"

func TestExactZeroPatchRequirementDoesNotAcceptAnotherPatch(t *testing.T) {
	requirement, err := VersionRequirement("toolchain.rust", "1.95.0")
	if err != nil {
		t.Fatal(err)
	}
	ids, err := VersionCapabilityIDs("toolchain.rust", "1.95.1")
	if err != nil {
		t.Fatal(err)
	}
	set, err := capabilitySetFromIDs(ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Missing([]Requirement{requirement})) != 1 {
		t.Fatal("an exact .0 pin accepted a different patch release")
	}
}
