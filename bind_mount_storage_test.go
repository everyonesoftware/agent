package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAccountStoragePreservesExistingPermissionsAndPersists(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "accounts.sqlite")
	store, err := newAccountStore(path)
	if err != nil {
		t.Fatal(err)
	}
	user, token, err := store.register("bind-user", "correct-horse-battery")
	if err != nil {
		_ = store.db.Close()
		t.Fatal(err)
	}
	if err := store.db.Close(); err != nil {
		t.Fatal(err)
	}
	assertStorageMode(t, dir, 0777)
	assertStorageMode(t, path, 0600)
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	reopened, err := newAccountStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.db.Close()
	loaded, err := reopened.userForToken(token)
	if err != nil || loaded.ID != user.ID {
		t.Fatalf("session did not persist: %v", err)
	}
	assertStorageMode(t, dir, 0777)
	assertStorageMode(t, path, 0666)
}

func TestNewAccountStorageRequestsPrivatePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new-data")
	store, err := newAccountStore(filepath.Join(dir, "accounts.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	assertStorageMode(t, dir, 0700)
	assertStorageMode(t, filepath.Join(dir, "accounts.sqlite"), 0600)
}

func TestCredentialsPreserveExistingDirectoryPermissions(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "profiles"
		if legacy {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0777); err != nil {
				t.Fatal(err)
			}
			credentials := testOAuthCredentials("fixture-access", "fixture-refresh", time.Now().Add(time.Hour))
			store := newFileCredentialStore(dir)
			if legacy {
				data, err := json.Marshal(credentials)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "siwc-token.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := store.Save(credentials); err != nil {
				t.Fatal(err)
			}
			loaded, err := newFileCredentialStore(dir).Load()
			if err != nil {
				t.Fatalf("reload credentials: %v", err)
			}
			if loaded.RefreshToken != credentials.RefreshToken {
				t.Fatal("persisted credentials changed")
			}
			id, err := store.HostID()
			if err != nil || id != credentials.HostID {
				t.Fatalf("host identity did not persist: %v", err)
			}
			if err := store.Save(loaded); err != nil {
				t.Fatalf("replace credentials: %v", err)
			}
			if reopenedID, err := newFileCredentialStore(dir).HostID(); err != nil || reopenedID != id {
				t.Fatalf("reopen host identity: %v", err)
			}
			assertStorageMode(t, dir, 0777)
			assertStorageMode(t, filepath.Join(dir, "siwc-profiles.json"), 0600)
			assertStorageMode(t, filepath.Join(dir, "host-id"), 0600)
		})
	}
}

func TestStorageStillRejectsInvalidPathsAndCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if store, err := newAccountStore(filepath.Join(path, "accounts.sqlite")); err == nil {
		_ = store.db.Close()
		t.Fatal("accepted unusable database path")
	}
	credentials := testOAuthCredentials("fixture-access", "fixture-refresh", time.Now().Add(time.Hour))
	if err := newFileCredentialStore(path).Save(credentials); err == nil {
		t.Fatal("accepted unusable credential directory")
	}
	if _, err := newFileCredentialStore(path).HostID(); err == nil {
		t.Fatal("accepted unusable host identity directory")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "siwc-profiles.json"), []byte(`{"version":-1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newFileCredentialStore(dir).Load(); err == nil {
		t.Fatal("bypassed credential validation")
	}
}

func TestCredentialStoreRejectsBroadLegacyPermissions(t *testing.T) {
	dir := t.TempDir()
	credentials := testOAuthCredentials("fixture-access", "fixture-refresh", time.Now().Add(time.Hour))
	data, err := json.Marshal(credentials)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "siwc-token.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := newFileCredentialStore(dir).Load(); err == nil {
		t.Fatal("loaded a group/world-readable legacy credential file")
	}
}

func assertStorageMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s permissions = %04o, want %04o", filepath.Base(path), got, want)
	}
}
