package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/google/go-github/v62/github"
)

func repoGitDir(persistPath string, repo *github.PushEventRepository) string {
	return filepath.Join(persistPath, "repos", "github.com", *repo.FullName)
}

func syncRepo(ctx context.Context, client *github.Client, repo *github.PushEventRepository) (gitDir string, err error) {
	_, _, _, err = client.Repositories.GetContents(ctx, *repo.Owner.Login, *repo.Name, ".github/commit-emails.json", nil)
	if err != nil {
		if _, ok := err.(*github.RateLimitError); ok {
			return "", fmt.Errorf("rate limit error: %s", err)
		}
		if _, ok := err.(*github.AbuseRateLimitError); ok {
			return "", fmt.Errorf("rate limit error: %s", err)
		}
		// TODO: only do this for 404
		return "", MissingConfigError{}
	}
	gitDir = repoGitDir(Cfg.PersistPath, repo)
	fi, err := os.Stat(gitDir)
	if os.IsNotExist(err) {
		err := gitClone(*repo.CloneURL, gitDir)
		if err != nil {
			return "", err
		}
		slog.Info("clone", slog.String("repo", *repo.FullName))
	} else if err != nil {
		return "", err
	} else if !fi.IsDir() {
		return "", fmt.Errorf("%s exists and is not a directory", gitDir)
	}

	err = gitFetch(gitDir)
	if err != nil {
		return
	}
	return
}

func gitClone(url string, dest string) error {
	_, err := runGitCmd(dest, "clone", "--bare", "--quiet", url, dest)
	return err
}

func gitFetch(gitDir string) error {
	_, err := runGitCmd(gitDir, "fetch", "--quiet", "--force", "origin", "*:*")
	return err
}

func runGitCmd(gitDir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_DIR="+gitDir)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return out, fmt.Errorf("git %v failed: %s: %q", args, ee.ProcessState.String(), ee.Stderr)
		}
	}
	return out, err
}
