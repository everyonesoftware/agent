package main

import (
	"strings"
	"testing"
)

func TestAgentBranding(t *testing.T) {
	for _, required := range []string{
		`<title>Agent</title>`, `placeholder="Message Agent"`,
		`New to Agent?`, `Create your personal Agent account.`,
		`sender.textContent = role === 'user' ? 'You' : 'Agent'`,
	} {
		if !strings.Contains(homePage, required) {
			t.Errorf("Agent branding is missing %s", required)
		}
	}
	if strings.Contains(homePage, "EveryoneAgent") {
		t.Error("page still contains the old display name")
	}
	if count := strings.Count(homePage, `<span class="mark" aria-hidden="true"></span>Agent`); count != 3 {
		t.Errorf("loading, login and sidebar must all use the Classic ring Agent lockup; got %d", count)
	}
	for _, required := range []string{`url('/icons/agent.svg')`, `<link rel="icon" type="image/svg+xml" href="/icons/agent.svg">`} {
		if !strings.Contains(homePage, required) {
			t.Errorf("Classic ring artwork is missing %s", required)
		}
	}
	for _, removed := range []string{`>e1</span>`, `content: 'e1'`} {
		if strings.Contains(homePage, removed) {
			t.Errorf("page still contains retired e1 artwork: %s", removed)
		}
	}
	if siwcAgentName != "Agent" {
		t.Errorf("new ChatGPT registrations should display Agent, got %q", siwcAgentName)
	}
	// A visual rebrand must not invalidate sessions or saved model preferences.
	if authCookieName != "everyoneagent_session" {
		t.Error("existing session cookie name must be preserved")
	}
	if !strings.Contains(homePage, "'everyoneagent.model.' + user.id") {
		t.Error("existing per-user model preference key must be preserved")
	}
}

func TestHomePageUsesFocusWorkspace(t *testing.T) {
	for _, required := range []string{
		`content="dark"`, `id="session-sidebar"`, `id="conversation-list"`,
		`id="conversation-search"`, `id="current-username"`, `id="account-menu"`,
		`id="account-menu-toggle"`, `id="sidebar-toggle"`, `id="sidebar-backdrop"`,
		`id="conversation-title"`, `#202322`, `#181b1a`, `#b4d6a3`,
	} {
		if !strings.Contains(homePage, required) {
			t.Errorf("Focus workspace is missing %s", required)
		}
	}
	if strings.Contains(homePage, `id="conversation-select"`) {
		t.Error("saved conversations should be sidebar buttons, not a dropdown")
	}
}

func TestHomePageUsesQuietEntryAuthentication(t *testing.T) {
	for _, required := range []string{
		`Welcome back.`, `id="auth-intro"`, `id="toggle-password"`,
		`id="password-hint"`, `autocomplete="username"`, `autocomplete="current-password"`,
	} {
		if !strings.Contains(homePage, required) {
			t.Errorf("Quiet entry authentication is missing %s", required)
		}
	}
	for _, preserved := range []string{
		`id="account-form"`, `id="username-input"`, `id="password-input"`,
		`id="auth-error"`, `id="toggle-account-mode"`, `id="connect-chatgpt"`,
		`id="account-select"`, `id="model-select"`, `id="logout-button"`,
	} {
		if !strings.Contains(homePage, preserved) {
			t.Errorf("existing authentication or provider control missing: %s", preserved)
		}
	}
}

func TestLoginPageOmitsSupplementaryCopy(t *testing.T) {
	for _, removed := range []string{
		`Your personal conversation space`,
		`Connect your ChatGPT account after signing in.`,
		`Agent · Your own space`,
	} {
		if strings.Contains(homePage, removed) {
			t.Errorf("login page still contains %q", removed)
		}
	}
}
