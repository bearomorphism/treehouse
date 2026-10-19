package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetSetupProgress(t *testing.T) {
	for _, mode := range []string{"interactive", "path", "json"} {
		t.Run(mode, func(t *testing.T) {
			repoDir, homeDir := setupTestRepo(t)
			writeProgressHook(t, homeDir)
			args := []string{"get"}
			if mode != "interactive" {
				args = append(args, "--lease")
			}
			if mode == "json" {
				args = append(args, "--json")
			}
			stdout, stderr, code := runTreehouse(t, repoDir, homeDir, []string{"SHELL=" + exitShellBin}, args...)
			if code != 0 {
				t.Fatalf("get failed (%d): %s", code, stderr)
			}
			steps := []string{"🌳 Setting up worktree...", "🌳 Fetching from origin...", "🌳 Creating worktree...", "🌳 Running post-create hooks..."}
			if mode != "interactive" {
				// Lease mode routes hook stdout to stderr too. Its first output
				// must follow the announcement, not precede a buffered summary.
				steps = append(steps, "progress-hook-started")
			}
			steps = append(steps, "🌳 Done.")
			assertProgressOrder(t, stderr, steps...)
			if strings.Contains(stdout, "🌳 Fetching") || strings.Contains(stdout, "🌳 Creating") || strings.Contains(stdout, "🌳 Running post-create") || strings.Contains(stdout, "🌳 Done.") {
				t.Fatalf("setup progress leaked to stdout: %q", stdout)
			}
			if strings.Contains(stderr, "🌳 Reusing worktree...") {
				t.Fatalf("fresh acquisition announced reuse: %s", stderr)
			}
			if mode == "interactive" {
				if !strings.Contains(stdout, "progress-hook-started") {
					t.Fatalf("interactive hook stdout missing: %q", stdout)
				}
				return
			}
			path := strings.TrimSpace(stdout)
			if mode == "json" {
				var lease leaseJSONResult
				if err := json.Unmarshal([]byte(stdout), &lease); err != nil {
					t.Fatalf("invalid lease JSON: %v: %q", err, stdout)
				}
				path = lease.Path
			}
			if !filepath.IsAbs(path) {
				t.Fatalf("lease path is not absolute: %q", path)
			}
			if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
				t.Fatalf("stdout did not identify a worktree: %v: %q", err, stdout)
			}
		})
	}
}

func TestGetSetupProgressOmitsSkippedSteps(t *testing.T) {
	for _, noRemote := range []bool{false, true} {
		name := "no-fetch"
		if noRemote {
			name = "no-origin"
		}
		t.Run(name, func(t *testing.T) {
			repoDir, homeDir := setupTestRepo(t)
			args := []string{"get", "--lease"}
			if noRemote {
				gitCmd(t, repoDir, "remote", "remove", "origin")
			} else {
				args = append(args, "--no-fetch")
			}
			_, stderr, code := runTreehouse(t, repoDir, homeDir, nil, args...)
			if code != 0 {
				t.Fatalf("get failed (%d): %s", code, stderr)
			}
			assertProgressOrder(t, stderr, "🌳 Setting up worktree...", "🌳 Creating worktree...", "🌳 Done.")
			for _, skipped := range []string{"🌳 Fetching from origin...", "🌳 Running post-create hooks...", "🌳 Reusing worktree..."} {
				if strings.Contains(stderr, skipped) {
					t.Errorf("announced skipped step %q: %s", skipped, stderr)
				}
			}
		})
	}
}

func TestGetSetupProgressReusedWorktree(t *testing.T) {
	repoDir, homeDir := setupTestRepo(t)
	writeProgressHook(t, homeDir)
	stdout, stderr, code := runTreehouse(t, repoDir, homeDir, nil, "get", "--lease")
	if code != 0 {
		t.Fatalf("initial get failed (%d): %s", code, stderr)
	}
	path := strings.TrimSpace(stdout)
	if _, stderr, code := runTreehouse(t, repoDir, homeDir, nil, "return", path); code != 0 {
		t.Fatalf("return failed (%d): %s", code, stderr)
	}
	stdout, stderr, code = runTreehouse(t, repoDir, homeDir, nil, "get", "--lease")
	if code != 0 || strings.TrimSpace(stdout) != path {
		t.Fatalf("did not reuse worktree (%d): stdout %q, stderr %q", code, stdout, stderr)
	}
	assertProgressOrder(t, stderr, "🌳 Setting up worktree...", "🌳 Fetching from origin...", "🌳 Reusing worktree...", "🌳 Running post-create hooks...", "progress-hook-started", "🌳 Done.")
	if strings.Contains(stderr, "🌳 Creating worktree...") {
		t.Fatalf("reuse announced creation: %s", stderr)
	}
}

func TestGetSetupProgressStopsOnFetchFailure(t *testing.T) {
	repoDir, homeDir := setupTestRepo(t)
	writeProgressHook(t, homeDir)
	gitCmd(t, repoDir, "remote", "set-url", "origin", filepath.Join(homeDir, "missing-remote.git"))
	stdout, stderr, code := runTreehouse(t, repoDir, homeDir, nil, "get", "--lease")
	if code == 0 || stdout != "" {
		t.Fatalf("failed fetch returned success or stdout (%d): %q", code, stdout)
	}
	assertProgressOrder(t, stderr, "🌳 Setting up worktree...", "🌳 Fetching from origin...", "fetch failed")
	for _, skipped := range []string{"🌳 Creating worktree...", "🌳 Reusing worktree...", "🌳 Running post-create hooks...", "progress-hook-started", "🌳 Done."} {
		if strings.Contains(stderr, skipped) {
			t.Errorf("failed fetch announced later step %q: %s", skipped, stderr)
		}
	}
}

func writeProgressHook(t *testing.T, homeDir string) {
	t.Helper()
	configDir := filepath.Join(homeDir, ".config", "treehouse")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[hooks]\npost_create = [\"echo progress-hook-started\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertProgressOrder(t *testing.T, stderr string, steps ...string) {
	t.Helper()
	rest := stderr
	for _, step := range steps {
		index := strings.Index(rest, step)
		if index < 0 {
			t.Fatalf("missing or out-of-order step %q in stderr: %s", step, stderr)
		}
		rest = rest[index+len(step):]
	}
}
