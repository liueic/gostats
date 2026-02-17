package provider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileRefreshTokenStoreSaveAndLoad(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "spotify_refresh_token")
	store := NewFileRefreshTokenStore(path)

	const token = "refresh_token_123"
	if err := store.Save(context.Background(), token); err != nil {
		t.Fatalf("save token: %v", err)
	}

	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load token: %v", err)
	}
	if got != token {
		t.Fatalf("unexpected token: got %q want %q", got, token)
	}
}

func TestFileRefreshTokenStoreLoadMissingFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing_token_file")
	store := NewFileRefreshTokenStore(path)

	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load missing file: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty token, got %q", got)
	}
}

func TestCommandRefreshTokenStoreSave(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "persisted")
	store := NewCommandRefreshTokenStore("printf '%s' \"$SPOTIFY_REFRESH_TOKEN\" > " + shellQuote(path))

	const token = "refresh_token_abc"
	if err := store.Save(context.Background(), token); err != nil {
		t.Fatalf("save token with command: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted token: %v", err)
	}
	if string(raw) != token {
		t.Fatalf("unexpected persisted token: got %q want %q", string(raw), token)
	}
}

func TestMultiRefreshTokenStoreLoadPrefersFirstNonEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path1 := filepath.Join(dir, "one")
	path2 := filepath.Join(dir, "two")
	store1 := NewFileRefreshTokenStore(path1)
	store2 := NewFileRefreshTokenStore(path2)

	if err := store2.Save(context.Background(), "token_two"); err != nil {
		t.Fatalf("save token two: %v", err)
	}
	if err := store1.Save(context.Background(), "token_one"); err != nil {
		t.Fatalf("save token one: %v", err)
	}

	multi := NewMultiRefreshTokenStore(store1, store2)
	got, err := multi.Load(context.Background())
	if err != nil {
		t.Fatalf("load token from multi store: %v", err)
	}
	if got != "token_one" {
		t.Fatalf("unexpected token: got %q want %q", got, "token_one")
	}
}

func TestFileRefreshTokenStoreEmptyPathErrors(t *testing.T) {
	t.Parallel()

	store := NewFileRefreshTokenStore(" ")
	if _, err := store.Load(context.Background()); err == nil || !strings.Contains(err.Error(), "path is empty") {
		t.Fatalf("expected empty-path load error, got %v", err)
	}
	if err := store.Save(context.Background(), "token"); err == nil || !strings.Contains(err.Error(), "path is empty") {
		t.Fatalf("expected empty-path save error, got %v", err)
	}
}

func TestCommandRefreshTokenStoreLoadNoop(t *testing.T) {
	t.Parallel()

	store := NewCommandRefreshTokenStore("echo test")
	token, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load command store should not fail: %v", err)
	}
	if token != "" {
		t.Fatalf("expected empty token from command store load, got %q", token)
	}
}

func TestCommandRefreshTokenStoreSaveValidationAndFailure(t *testing.T) {
	t.Parallel()

	store := NewCommandRefreshTokenStore(" ")
	if err := store.Save(context.Background(), "token"); err == nil || !strings.Contains(err.Error(), "persist command is empty") {
		t.Fatalf("expected empty command error, got %v", err)
	}

	store = NewCommandRefreshTokenStore("echo failed >&2; exit 7")
	err := store.Save(context.Background(), "token")
	if err == nil || !strings.Contains(err.Error(), "run persist command") || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("expected command failure with stderr output, got %v", err)
	}
}

func TestMultiRefreshTokenStoreLoadAndSaveErrors(t *testing.T) {
	t.Parallel()

	loadErrStore := &testRefreshStore{loadErr: errors.New("load boom")}
	multi := NewMultiRefreshTokenStore(loadErrStore)
	if _, err := multi.Load(context.Background()); err == nil || !strings.Contains(err.Error(), "load boom") {
		t.Fatalf("expected load error propagation, got %v", err)
	}

	saveErrStore := &testRefreshStore{saveErr: errors.New("save boom")}
	multi = NewMultiRefreshTokenStore(saveErrStore)
	if err := multi.Save(context.Background(), "token"); err == nil || !strings.Contains(err.Error(), "save boom") {
		t.Fatalf("expected save error propagation, got %v", err)
	}
}

func TestMultiRefreshTokenStoreSaveAllStores(t *testing.T) {
	t.Parallel()

	storeA := &testRefreshStore{}
	storeB := &testRefreshStore{}
	multi := NewMultiRefreshTokenStore(nil, storeA, storeB)

	if err := multi.Save(context.Background(), "token-all"); err != nil {
		t.Fatalf("save to multi store: %v", err)
	}
	if len(storeA.saved) != 1 || storeA.saved[0] != "token-all" {
		t.Fatalf("storeA did not persist token correctly: %+v", storeA.saved)
	}
	if len(storeB.saved) != 1 || storeB.saved[0] != "token-all" {
		t.Fatalf("storeB did not persist token correctly: %+v", storeB.saved)
	}
}

func shellQuote(value string) string {
	return "'" + filepath.ToSlash(value) + "'"
}
