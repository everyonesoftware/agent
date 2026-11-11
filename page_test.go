package main

import (
	"strings"
	"testing"
)

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
		`Connect your ChatGPT account after signing in.`,
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
