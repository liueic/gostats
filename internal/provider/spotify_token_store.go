package provider

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type RefreshTokenStore interface {
	Load(ctx context.Context) (string, error)
	Save(ctx context.Context, refreshToken string) error
}

type FileRefreshTokenStore struct {
	path string
}

func NewFileRefreshTokenStore(path string) *FileRefreshTokenStore {
	return &FileRefreshTokenStore{
		path: strings.TrimSpace(path),
	}
}

func (s *FileRefreshTokenStore) Load(_ context.Context) (string, error) {
	if strings.TrimSpace(s.path) == "" {
		return "", fmt.Errorf("refresh token file path is empty")
	}

	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read refresh token file: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

func (s *FileRefreshTokenStore) Save(_ context.Context, refreshToken string) error {
	if strings.TrimSpace(s.path) == "" {
		return fmt.Errorf("refresh token file path is empty")
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create refresh token directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".spotify-refresh-token-*")
	if err != nil {
		return fmt.Errorf("create temp refresh token file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		_ = tmp.Close()
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod temp refresh token file: %w", err)
	}
	if _, err := tmp.WriteString(strings.TrimSpace(refreshToken) + "\n"); err != nil {
		return fmt.Errorf("write refresh token file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync refresh token file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close refresh token file: %w", err)
	}

	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace refresh token file: %w", err)
	}
	cleanup = false
	return nil
}

type CommandRefreshTokenStore struct {
	command string
}

func NewCommandRefreshTokenStore(command string) *CommandRefreshTokenStore {
	return &CommandRefreshTokenStore{
		command: strings.TrimSpace(command),
	}
}

func (s *CommandRefreshTokenStore) Load(_ context.Context) (string, error) {
	return "", nil
}

func (s *CommandRefreshTokenStore) Save(ctx context.Context, refreshToken string) error {
	if strings.TrimSpace(s.command) == "" {
		return fmt.Errorf("persist command is empty")
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", s.command)
	cmd.Env = append(os.Environ(), "SPOTIFY_REFRESH_TOKEN="+strings.TrimSpace(refreshToken))

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		output := strings.TrimSpace(out.String())
		if output != "" {
			return fmt.Errorf("run persist command: %w: %s", err, output)
		}
		return fmt.Errorf("run persist command: %w", err)
	}
	return nil
}

type MultiRefreshTokenStore struct {
	stores []RefreshTokenStore
}

func NewMultiRefreshTokenStore(stores ...RefreshTokenStore) *MultiRefreshTokenStore {
	filtered := make([]RefreshTokenStore, 0, len(stores))
	for _, store := range stores {
		if store != nil {
			filtered = append(filtered, store)
		}
	}
	return &MultiRefreshTokenStore{
		stores: filtered,
	}
}

func (s *MultiRefreshTokenStore) Load(ctx context.Context) (string, error) {
	for _, store := range s.stores {
		token, err := store.Load(ctx)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(token) != "" {
			return strings.TrimSpace(token), nil
		}
	}
	return "", nil
}

func (s *MultiRefreshTokenStore) Save(ctx context.Context, refreshToken string) error {
	for _, store := range s.stores {
		if err := store.Save(ctx, refreshToken); err != nil {
			return err
		}
	}
	return nil
}
