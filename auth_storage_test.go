package main

import (
	"strings"
	"testing"
)

func TestAccountsAndSessionsPersistInSQLite(t *testing.T) {
	path := t.TempDir() + "/accounts.sqlite"
	store, err := newAccountStore(path)
	if err != nil {
		t.Fatal(err)
	}
	user, token, err := store.register("persisted", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newAccountStore(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.userForToken(token)
	if err != nil || loaded.ID != user.ID {
		t.Fatalf("persisted session user=%#v err=%v; want %q", loaded, err, user.ID)
	}
}

func TestChatGPTCredentialStoresArePerUser(t *testing.T) {
	dir := t.TempDir()
	accounts, err := newAccountStore(dir + "/accounts.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := accounts.register("first", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := accounts.register("second", "another-correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	base := newSIWCService(siwcConfig{}, newFileCredentialStore(dir+"/legacy"))
	firstStore := accounts.siwcForUser(base, first).store.(*fileCredentialStore)
	secondStore := accounts.siwcForUser(base, second).store.(*fileCredentialStore)
	if firstStore.dir == secondStore.dir {
		t.Fatal("two users share the same ChatGPT credential store")
	}
	if !strings.Contains(firstStore.dir, first.ID) || !strings.Contains(secondStore.dir, second.ID) {
		t.Fatalf("credential stores are not scoped by account: %q %q", firstStore.dir, secondStore.dir)
	}
}
