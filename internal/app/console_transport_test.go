package app

import "testing"

func TestResolveTransportEnablesSecureConsoleCookiesForHTTPSPublicURL(t *testing.T) {
	clearRuntimeEnv(t)
	t.Setenv(tokenEnv, "transport-console-token")
	t.Setenv(publicURLEnv, "https://console.example.test")
	t.Setenv(oauthPassphraseEnv, "owner-passphrase-with-sufficient-entropy")

	transport, err := resolveTransport(serveOptions{HTTPAddr: ":8765"})
	if err != nil {
		t.Fatal(err)
	}
	if !transport.ConsoleSecureCookies {
		t.Fatal("HTTPS public URL did not enable secure console cookies")
	}
}

func TestResolveTransportLeavesConsoleCookieInferenceForLocalHTTP(t *testing.T) {
	clearRuntimeEnv(t)
	t.Setenv(tokenEnv, "transport-console-token")

	transport, err := resolveTransport(serveOptions{HTTPAddr: ":8765"})
	if err != nil {
		t.Fatal(err)
	}
	if transport.ConsoleSecureCookies {
		t.Fatal("local HTTP unexpectedly forced secure console cookies")
	}
}
