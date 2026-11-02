package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthenticatedOAuthCallbackAllowsCrossSiteProviderRedirect(t *testing.T) {
	accounts, err := newAccountStore(t.TempDir() + "/accounts.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	user, token, err := accounts.register("oauth-user", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	base := newSIWCService(siwcConfig{}, newFileCredentialStore(t.TempDir()))
	called := false
	handler := accounts.scopedOAuthCallbackHandler(base, func(_ *siwcService, w http.ResponseWriter, r *http.Request) {
		if got, ok := currentAuthUser(r.Context()); !ok || got.ID != user.ID {
			t.Errorf("callback user=%#v authenticated=%v", got, ok)
		}
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/auth/callback?code=provider-code", nil)
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	request.AddCookie(&http.Cookie{Name: authCookieName, Value: token})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || !called {
		t.Fatalf("callback status=%d called=%v body=%s", response.Code, called, response.Body.String())
	}
}
