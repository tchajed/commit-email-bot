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

func tokenToParams(token string) []gitConfigParam {
	return []gitConfigParam{
		{
			Key:   "credential.https://github.com.username",
			Value: "x-access-token",
		},
		{
			Key:   "http.https://github.com.extraheader",
			Value: fmt.Sprintf("Authorization: %s", token),
		}}
}

func SyncRepo(ctx context.Context, client *github.Client, repo *github.PushEventRepository) (gitDir string, err error) {
	_, _, _, err = client.Repositories.GetContents(ctx, *repo.Owner.Login, *repo.Name, ".github/commit-emails.json", nil)
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
