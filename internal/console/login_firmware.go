package console

import (
	"io"
	"net/http"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/authfirmware"
)

func (h *Handler) writeLoginPage(w http.ResponseWriter, failed bool) {
	authfirmware.Harden(w, authfirmware.CSP)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	status := http.StatusOK
	var page strings.Builder
	page.WriteString(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark">
<meta name="theme-color" content="#0000A8">
<title>MCP Devbox Firmware — Console Authentication</title>
<link rel="stylesheet" href="` + authfirmware.Path + `">
</head>
<body>
<main class="firmware-auth" aria-labelledby="firmware-title">
<header class="firmware-titlebar"><p>MCP DEVBOX AUTH FIRMWARE</p><p>[ AUTH ]</p></header>
<section class="firmware-screen">
<h1 id="firmware-title">MCP Devbox Console Authentication</h1>
<p class="firmware-lead">Private presentation surface. Choose the normal OAuth path or use the recovery credential when OAuth is unavailable.</p>
`)
	if failed {
		status = http.StatusUnauthorized
		page.WriteString(`<p class="firmware-alert" role="alert">[ DENIED ] Authentication failed.</p>`)
	}
	if h.oauthClient != nil {
		page.WriteString(`<section class="firmware-panel" aria-labelledby="oauth-title">
<h2 id="oauth-title">[ READY ] OAuth sign-in</h2>
<p>Recommended path. The owner passphrase is entered only on the authorization firmware page; access tokens never reach console JavaScript.</p>
<a class="firmware-action firmware-action--primary" href="/console/auth/start">Sign in with OAuth</a>
</section>`)
	} else {
		page.WriteString(`<section class="firmware-panel" aria-labelledby="oauth-title">
<h2 id="oauth-title">[ OFFLINE ] OAuth sign-in</h2>
<p class="firmware-note">OAuth is not configured for this console.</p>
</section>`)
	}
	if h.staticToken != "" {
		page.WriteString(`<section class="firmware-panel firmware-panel--secondary" aria-labelledby="recovery-title">
<h2 id="recovery-title">[ RECOVERY ] Bearer access</h2>
<p class="firmware-note">Use only as a recovery path. This value is not an OAuth credential and is submitted in the HTTPS request body.</p>
<form method="post" action="/console/login">
<label for="token">Recovery bearer token</label>
<input id="token" name="token" type="password" required maxlength="4096" autocomplete="current-password" inputmode="text">
<button class="firmware-action" type="submit">Sign in with bearer recovery</button>
</form>
</section>`)
	}
	if h.oauthClient == nil && h.staticToken == "" {
		page.WriteString(`<p class="firmware-alert" role="alert">[ LOCKED ] No console authentication method is configured.</p>`)
	}
	page.WriteString(`</section>
<footer class="firmware-keybar"><p><b>ENTER</b> select</p><p><b>TAB</b> move focus</p><p>[ PRESENTATION ONLY ]</p></footer>
</main>
</body>
</html>`)
	w.WriteHeader(status)
	_, _ = io.WriteString(w, page.String())
}
