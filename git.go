package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
)

func syncRepo(gitDir string, url string) error {
	fi, err := os.Stat(gitDir)
	if os.IsNotExist(err) {
		err := gitClone(url, gitDir)
		if err != nil {
			return err
		}
		log.Printf("Cloned %s to %s", url, gitDir)
	} else if err != nil {
		return err
	} else if !fi.IsDir() {
		return fmt.Errorf("%s exists and is not a directory", gitDir)
	}

	err = gitFetch(gitDir)
	if err != nil {
		return err
	}

	return nil
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
