package catalogrollout

import (
	"strings"
	"testing"
	"time"
)

func TestPublishedStatusRoundTripIsCompactAndStrict(t *testing.T) {
	status := Status{
		Revision: 7, Request: tr(), State: StateRunning, Phase: PhaseDeployBackend,
		DeploymentID: "deployment_1", UpdatedAt: time.Unix(1234, 0).UTC(),
	}
	description, err := EncodePublishedStatus(status)
	if err != nil || len(description) > 255 {
		t.Fatalf("description=%q err=%v", description, err)
	}
	decoded, ok, err := DecodePublishedStatus(description)
	if err != nil || !ok || decoded.Revision != status.Revision || decoded.Request.RequestID != status.Request.RequestID || decoded.State != status.State || decoded.Phase != status.Phase || decoded.DeploymentID != status.DeploymentID {
		t.Fatalf("decoded=%+v ok=%t err=%v", decoded, ok, err)
	}
	if _, ok, err := DecodePublishedStatus("other"); ok || err != nil {
		t.Fatalf("unexpected decode ok=%t err=%v", ok, err)
	}
	for _, invalid := range []string{
		PublishedStatusPrefix + `{}`,
		PublishedStatusPrefix + `{"q":"` + status.Request.RequestID + `","s":"running","u":1,"extra":true}`,
		PublishedStatusPrefix + strings.Repeat("x", 300),
	} {
		if _, ok, err := DecodePublishedStatus(invalid); !ok || err == nil {
			t.Fatalf("invalid status accepted: %q", invalid)
		}
	}
}
