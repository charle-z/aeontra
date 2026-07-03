package oauth

import (
	"crypto/subtle"
	"html/template"
	"net/http"
	"net/url"
	"time"
)

// authCodeTTL is how long an authorization code is valid. Codes are single-use and
// short-lived to bound the window for interception (OAuth 2.1 §7.5).
const authCodeTTL = 60 * time.Second

// authorizeParams are the validated authorization-request parameters.
type authorizeParams struct {
	clientID      string
	redirectURI   string
	codeChallenge string
	state         string
	scope         string
	resource      string
}

// validateAuthorizeRequest checks every authorization parameter against the spec and the
// registered client. It is called for BOTH the GET (render) and POST (submit) so the
// POST never trusts hidden form fields — it re-derives everything from scratch.
func (p *Provider) validateAuthorizeRequest(v url.Values) (*authorizeParams, error) {
	clientID := v.Get("client_id")
	client, ok := p.store.getClient(clientID)
	if !ok {
		return nil, errorString("unknown client_id")
	}
	redirectURI := v.Get("redirect_uri")
	if !registeredRedirect(client, redirectURI) {
		return nil, errorString("redirect_uri does not match a registered value")
	}
	if v.Get("response_type") != "code" {
		return nil, errorString("response_type must be code")
	}
	if v.Get("code_challenge") == "" {
		return nil, errorString("code_challenge is required (PKCE)")
	}
	if v.Get("code_challenge_method") != "S256" {
		return nil, errorString("code_challenge_method must be S256")
	}
	if res := v.Get("resource"); res != p.resource {
		return nil, errorString("resource must match this MCP server's canonical URI")
	}
	return &authorizeParams{
		clientID:      clientID,
		redirectURI:   redirectURI,
		codeChallenge: v.Get("code_challenge"),
		state:         v.Get("state"),
		scope:         v.Get("scope"),
		resource:      v.Get("resource"),
	}, nil
}

func registeredRedirect(c clientReg, uri string) bool {
	for _, u := range c.redirectURIs {
		if subtle.ConstantTimeCompare([]byte(u), []byte(uri)) == 1 {
			return true
		}
	}
	return false
}

// handleAuthorize renders the owner login page (GET) and processes the login (POST).
func (p *Provider) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		params, err := p.validateAuthorizeRequest(r.URL.Query())
		if err != nil {
			// Do NOT redirect on a validation failure: an unvalidated client_id or
			// redirect_uri could be attacker-controlled (open-redirect protection).
			http.Error(w, "invalid authorization request: "+err.Error(), http.StatusBadRequest)
			return
		}
		renderLogin(w, params, "")
	case http.MethodPost:
		if p.store.passphraseThrottled() {
			http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		// Re-validate every parameter from the POST body — hidden fields are untrusted.
		params, err := p.validateAuthorizeRequest(r.PostForm)
		if err != nil {
			http.Error(w, "invalid authorization request: "+err.Error(), http.StatusBadRequest)
			return
		}
		// Constant-time passphrase check; the passphrase is never logged.
		if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("passphrase")), []byte(p.passphrase)) != 1 {
			p.store.recordPassphraseFailure()
			renderLoginStatus(w, params, "Incorrect passphrase.", http.StatusUnauthorized)
			return
		}
		p.store.resetPassphraseFailures()

		code := randToken()
		p.store.putCode(code, authCode{
			clientID:      params.clientID,
			redirectURI:   params.redirectURI,
			codeChallenge: params.codeChallenge,
			scope:         params.scope,
			resource:      params.resource,
			expiresAt:     time.Now().Add(authCodeTTL),
		})
		redirectWithCode(w, r, params, code)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// redirectWithCode sends the browser back to the client's redirect_uri with the
// authorization code and the original state parameter.
func redirectWithCode(w http.ResponseWriter, r *http.Request, params *authorizeParams, code string) {
	u, err := url.Parse(params.redirectURI)
	if err != nil { // already validated at registration + authorize, but be defensive
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	q := u.Query()
	q.Set("code", code)
	if params.state != "" {
		q.Set("state", params.state)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// loginTemplate is a minimal owner-login page. html/template auto-escapes every value,
// so the (already-validated) parameters carried as hidden fields cannot inject markup.
var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>mcp-devbox — authorize</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>body{font-family:system-ui,sans-serif;max-width:24rem;margin:4rem auto;padding:0 1rem}
input{width:100%;padding:.6rem;margin:.3rem 0 1rem;box-sizing:border-box}
button{padding:.6rem 1rem}.err{color:#b00020}</style></head>
<body>
<h1>Authorize access</h1>
<p>Sign in to let this client connect to your mcp-devbox.</p>
{{if .Status}}<p class="err">{{.Status}}</p>{{end}}
<form method="post" action="/oauth/authorize">
  <label>Passphrase<input type="password" name="passphrase" autofocus autocomplete="current-password"></label>
  <input type="hidden" name="response_type" value="code">
  <input type="hidden" name="client_id" value="{{.ClientID}}">
  <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
  <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
  <input type="hidden" name="code_challenge_method" value="S256">
  <input type="hidden" name="state" value="{{.State}}">
  <input type="hidden" name="scope" value="{{.Scope}}">
  <input type="hidden" name="resource" value="{{.Resource}}">
  <button type="submit">Authorize</button>
</form>
</body></html>`))

func renderLogin(w http.ResponseWriter, params *authorizeParams, status string) {
	renderLoginStatus(w, params, status, http.StatusOK)
}

func renderLoginStatus(w http.ResponseWriter, params *authorizeParams, status string, code int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_ = loginTemplate.Execute(w, map[string]string{
		"ClientID":      params.clientID,
		"RedirectURI":   params.redirectURI,
		"CodeChallenge": params.codeChallenge,
		"State":         params.state,
		"Scope":         params.scope,
		"Resource":      params.resource,
		"Status":        status,
	})
}
