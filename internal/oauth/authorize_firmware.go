package oauth

import (
	"html/template"
	"net/http"
	"net/url"

	"github.com/charle-z/mcp-devbox/internal/authfirmware"
)

var firmwareLoginTemplate = template.Must(template.New("firmware-login").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark">
<meta name="theme-color" content="#0000A8">
<title>MCP Devbox Firmware — OAuth Authorization</title>
<link rel="stylesheet" href="{{.Stylesheet}}">
</head>
<body>
<main class="firmware-auth" aria-labelledby="firmware-title">
<header class="firmware-titlebar"><p>MCP DEVBOX AUTH FIRMWARE</p><p>[ AUTH ]</p></header>
<section class="firmware-screen">
<h1 id="firmware-title">OAuth Authorization</h1>
<p class="firmware-lead">A registered client is requesting bounded access to the MCP resource.</p>
<p class="firmware-status" data-state="{{.StateTone}}">{{.StateLabel}}</p>
{{if .Status}}<p class="firmware-alert" role="alert">{{.Status}}</p>{{end}}
<section class="firmware-panel" aria-labelledby="request-title">
<h2 id="request-title">Authorization request</h2>
<dl class="firmware-grid">
<dt>Client</dt><dd>Registered OAuth client</dd>
<dt>Scope</dt><dd>{{.Scope}}</dd>
<dt>Resource</dt><dd>{{.ResourceLabel}}</dd>
</dl>
<form method="post" action="/oauth/authorize">
<label for="passphrase">Owner passphrase</label>
<input id="passphrase" type="password" name="passphrase" required maxlength="4096" autofocus autocomplete="current-password">
<p class="firmware-note">The passphrase is submitted only to this server and is never placed in the redirect URL.</p>
<input type="hidden" name="response_type" value="code">
<input type="hidden" name="client_id" value="{{.ClientID}}">
<input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
<input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
<input type="hidden" name="code_challenge_method" value="S256">
<input type="hidden" name="state" value="{{.State}}">
<input type="hidden" name="scope" value="{{.Scope}}">
<input type="hidden" name="resource" value="{{.Resource}}">
<button class="firmware-action firmware-action--primary" type="submit">Authorize client</button>
</form>
</section>
<p><a class="firmware-action" href="/console">Return to console</a></p>
</section>
<footer class="firmware-keybar"><p><b>ENTER</b> authorize</p><p><b>ESC</b> return</p><p>[ PKCE S256 ]</p></footer>
</main>
</body>
</html>`))

type firmwareLoginData struct {
	Stylesheet    string
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	State         string
	Scope         string
	Resource      string
	ResourceLabel string
	Status        string
	StateLabel    string
	StateTone     string
}

func renderLogin(w http.ResponseWriter, params *authorizeParams, status string) {
	renderLoginStatus(w, params, status, http.StatusOK)
}

func renderLoginStatus(w http.ResponseWriter, params *authorizeParams, status string, code int) {
	authfirmware.Harden(w, authfirmware.CSP)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	stateLabel := "[ READY ] Awaiting owner authorization"
	stateTone := "ready"
	if code == http.StatusTooManyRequests {
		stateLabel = "[ LOCKED ] Passphrase throttling active"
		stateTone = "locked"
	} else if status != "" {
		stateLabel = "[ DENIED ] Authorization not granted"
		stateTone = "locked"
	}
	w.WriteHeader(code)
	_ = firmwareLoginTemplate.Execute(w, firmwareLoginData{
		Stylesheet: authfirmware.Path, ClientID: params.clientID,
		RedirectURI: params.redirectURI, CodeChallenge: params.codeChallenge,
		State: params.state, Scope: params.scope, Resource: params.resource,
		ResourceLabel: safeResourceLabel(params.resource), Status: status,
		StateLabel: stateLabel, StateTone: stateTone,
	})
}

func safeResourceLabel(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return "MCP resource"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
