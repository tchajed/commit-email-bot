package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v62/github"
)

func repoGitDir(persistPath string, repo *github.PushEventRepository) string {
	return filepath.Join(persistPath, "repos", "github.com", *repo.FullName)
}

// We authenticate to GitHub using an installation access token, acting as the
// bot itself (not the user). The documentation suggests to do this with the URL
// https://x-access-token:<token>@github.com/owner/repo.git, but this would
// store that URL in the git config in plaintext. The tokens are valid for 1
// hour, which is still a lot of exposure.
//
// Passing credentials via configuration, which we can pass by environment
// variable, is a bit tricky, but git provides a "credential helper" which is a
// program that gets credentials. We set that helper to a bash script that
// echoes the constant token as the password (and a dummy username, which is
// required), then pass it via the environment using git's GIT_CONFIG_*
// variables (see runGitCmd)
func tokenToCredentialHelper(token string) string {
	return fmt.Sprintf("!f() { echo \"username=x-access-token\"; echo \"password=%s\"; }; f", token)
}

func tokenToParams(token string) []gitConfigParam {
	return []gitConfigParam{{
		Key:   "credential.helper",
		Value: tokenToCredentialHelper(token),
	}}
}

func SyncRepo(ctx context.Context, client *github.Client, repo *github.PushEventRepository) (gitDir string, err error) {
	_, _, _, err = client.Repositories.GetContents(ctx, *repo.Owner.Login, *repo.Name, ".github/commit-emails.toml", nil)
	if err != nil {
		if _, ok := err.(*github.RateLimitError); ok {
			return "", fmt.Errorf("rate limit error: %s", err)
		}
		if _, ok := err.(*github.AbuseRateLimitError); ok {
			return "", fmt.Errorf("abuse limit error: %s", err)
		}
		// TODO: only do this for 404
		return "", MissingConfigError{}
	}

	// TODO: might not want to authenticate for public repos
	itr := client.Client().Transport.(*ghinstallation.Transport)
	token, err := itr.Token(ctx)
	if err != nil {
		return "", err
	}
	params := tokenToParams(token)

	gitDir = repoGitDir(Cfg.PersistPath, repo)
	fi, err := os.Stat(gitDir)
	if os.IsNotExist(err) {
		err := gitClone(*repo.CloneURL, gitDir, params)
		if err != nil {
			return "", err
		}
		slog.Info("clone", slog.String("repo", *repo.FullName))
	} else if err != nil {
		return "", err
	} else if !fi.IsDir() {
		return "", fmt.Errorf("%s exists and is not a directory", gitDir)
	}

	err = gitFetch(gitDir, params)
	if err != nil {
		return
	}
	return
}

// GitShow fetches the contents of a file
func GitShow(gitDir, ref, path string) ([]byte, error) {
	return runGitCmd(gitDir, nil, "show", ref+":"+path)
}

type gitConfigParam struct {
	Key   string
	Value string
}

func gitClone(url string, dest string, params []gitConfigParam) error {
	_, err := runGitCmd(dest, params, "clone", "--bare", "--quiet", url, dest)
	return err
}

func gitFetch(gitDir string, params []gitConfigParam) error {
	_, err := runGitCmd(gitDir, params, "fetch", "--quiet", "--force", "origin", "*:*")
	return err
}

func runGitCmd(gitDir string, params []gitConfigParam, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "GIT_DIR="+gitDir)
	// GIT_CONFIG_COUNT, GIT_CONFIG_KEY_<n>, GIT_CONFIG_VALUE_<n>, ... are a
	// feature to pass configuration options by environment variables. There's
	// also GIT_CONFIG_PARAMETERS, but it's hard to encode arbitrary values with
	// spaces in that.
	cmd.Env = append(cmd.Env, fmt.Sprintf("GIT_CONFIG_COUNT=%d", len(params)))
	for i, p := range params {
		cmd.Env = append(cmd.Env,
			fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, p.Key),
			fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, p.Value),
		)
	}
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return out, fmt.Errorf("git %v failed: %s: %q", args, ee.ProcessState.String(), ee.Stderr)
		}
	}
	return out, err
}
