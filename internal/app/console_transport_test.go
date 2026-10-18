package app

import "testing"

func TestResolveTransportCannotConfigureAnInsecureConsoleCookie(t *testing.T) {
	for _, publicURL := range []string{"", "http://localhost:8765", "https://console.example.test"} {
		clearRuntimeEnv(t)
		t.Setenv(tokenEnv, "transport-console-token")
		if publicURL != "" {
			t.Setenv(publicURLEnv, publicURL)
			t.Setenv(oauthPassphraseEnv, "owner-passphrase-with-sufficient-entropy")
		}
		transport, err := resolveTransport(serveOptions{HTTPAddr: ":8765"})
		if err != nil {
			t.Fatal(err)
		}
		if transport.Mode != transportHTTP {
			t.Fatalf("public URL %q changed transport mode", publicURL)
		}
	}
}
