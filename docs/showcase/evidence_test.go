package showcase

import (
	"bytes"
	"strings"
	"testing"
)

func TestEmbeddedPixelgramaEvidenceIsValidAndCurrent(t *testing.T) {
	data, err := PixelgramaEvidence()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ParsePixelgramaEvidence(data)
	if err != nil {
		t.Fatal(err)
	}

	const current = "c6eaeae4561c450459cf31b4dc6b4b560abf7cf2"
	if manifest.SchemaVersion != SchemaVersion {
		t.Fatalf("schema_version=%d", manifest.SchemaVersion)
	}
	if manifest.Project.ProductionURL != productionURL || manifest.Project.PrimaryPublicRoute != primaryPublicRoute || manifest.Project.VersionURL != versionURL {
		t.Fatalf("canonical Pixelgrama URLs changed: %#v", manifest.Project)
	}
	if manifest.ProductionObservation.ObservedCommit != current || manifest.ProductionObservation.SourceMainCommit != current || !manifest.ProductionObservation.MatchesSourceMain {
		t.Fatalf("production truth does not match current source main: %#v", manifest.ProductionObservation)
	}
	if manifest.ProductionObservation.VerifiedOn != "2026-07-28" {
		t.Fatalf("verified_on=%q", manifest.ProductionObservation.VerifiedOn)
	}
	if len(manifest.Project.PublicSessions) != 0 {
		t.Fatalf("public sessions were invented: %#v", manifest.Project.PublicSessions)
	}
}

func TestPixelgramaEvidenceRejectsUnknownFields(t *testing.T) {
	data := bytes.Replace(pixelgramaEvidence, []byte(`"schema_version": 1,`), []byte(`"schema_version": 1, "unexpected": true,`), 1)
	if _, err := ParsePixelgramaEvidence(data); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field error=%v", err)
	}
}

func TestPixelgramaEvidenceRejectsSensitiveContentAndPrivateIdentifiers(t *testing.T) {
	tests := []struct {
		name        string
		replacement string
		want        string
	}{
		{name: "credential", replacement: "Bear" + "er abcdefghijklmnopqrstuvwxyz", want: "bearer credential"},
		{name: "private identifier", replacement: "ws" + "_0123456789abcdef0123456789abcdef", want: "private identifier"},
		{name: "private path", replacement: "/sta" + "te/private", want: "private filesystem path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := bytes.Replace(pixelgramaEvidence, []byte("Build a public bilingual"), []byte(test.replacement), 1)
			if _, err := ParsePixelgramaEvidence(data); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("sensitive content error=%v", err)
			}
		})
	}
}

func TestPixelgramaEvidenceSeparatesHistoricalAndProductionTruth(t *testing.T) {
	otherSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	data := bytes.Replace(pixelgramaEvidence, []byte(`"observed_commit": "c6eaeae4561c450459cf31b4dc6b4b560abf7cf2"`), []byte(`"observed_commit": "`+otherSHA+`"`), 1)
	if _, err := ParsePixelgramaEvidence(data); err == nil || !strings.Contains(err.Error(), "explicitly match source main") {
		t.Fatalf("production mismatch error=%v", err)
	}
}

func TestPixelgramaEvidenceRejectsInvalidURLSHAAndInfrastructure(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{name: "URL", old: productionURL, new: "http://pixelgrama.invalid", want: "canonical URL"},
		{name: "SHA", old: "4900532c70ef2d14d65f9ab3a7ab9c5e58607ad5", new: "not-a-sha", want: "valid head SHA"},
		{name: "provider", old: `"provider": "CubePath"`, new: `"provider": "Other"`, want: "CubePath and Coolify"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := bytes.Replace(pixelgramaEvidence, []byte(test.old), []byte(test.new), 1)
			if _, err := ParsePixelgramaEvidence(data); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validation error=%v", err)
			}
		})
	}
}
