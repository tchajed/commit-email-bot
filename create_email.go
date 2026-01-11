package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/google/go-github/v62/github"
)

func gitDiffHtml(gitDir string, commitId string) (string, error) {
	gitCmd := exec.Command("git", "show", "--color=always", "--compact-summary", "--patch", "--pretty=format:%h|%B", commitId)
	var gitStderr bytes.Buffer
	gitCmd.Stderr = &gitStderr
	gitCmd.Dir = gitDir

	deltaCmd := exec.Command("delta", "--no-gitconfig", "--light")
	// Chain gitCmd to deltaCmd
	{
		gitOut, err := gitCmd.StdoutPipe()
		if err != nil {
			return "", fmt.Errorf("error creating git stdout pipe: %w", err)
		}
		deltaCmd.Stdin = gitOut
	}

	ahaCmd := exec.Command("aha")
	// chain deltaCmd to ahaCmd
	{
		deltaOut, err := deltaCmd.StdoutPipe()
		if err != nil {
			return "", fmt.Errorf("error creating delta stdout pipe: %w", err)
		}
		ahaCmd.Stdin = deltaOut
	}

	// get output of aha (end of pipe) into a buffer
	var output bytes.Buffer
	var stderrBuf bytes.Buffer
	ahaCmd.Stdout = &output
	ahaCmd.Stderr = &stderrBuf

	// Start all commands
	if err := gitCmd.Start(); err != nil {
		return "", fmt.Errorf("error starting git: %w", err)
	}
	if err := deltaCmd.Start(); err != nil {
		return "", fmt.Errorf("error starting delta: %w", err)
	}
	if err := ahaCmd.Start(); err != nil {
		return "", fmt.Errorf("error starting aha: %w", err)
	}

	// Wait for all commands to complete
	if err := gitCmd.Wait(); err != nil {
		return "", fmt.Errorf("git show failed: %w", err)
	}
	if err := deltaCmd.Wait(); err != nil {
		return "", fmt.Errorf("delta failed: %w", err)
	}
	if err := ahaCmd.Wait(); err != nil {
		return "", fmt.Errorf("aha failed: %w (stderr: %s)", err, stderrBuf.String())
	}

	return output.String(), nil
}

func commitToEmail(gitDir string, repo string, branch string, commit *github.HeadCommit) (*EmailMsg, error) {
	config, err := getConfig(gitDir)
	if err != nil {
		return nil, fmt.Errorf("could not get config for %s: %w", repo, err)
	}
	to := config.MailingList
	body, err := gitDiffHtml(gitDir, commit.GetID())
	if err != nil {
		return nil, err
	}
	msg, _, _ := strings.Cut(commit.GetMessage(), "\n")
	subject := fmt.Sprintf("%s %s: %s", repo, branch, msg)
	fromName := commit.GetAuthor().GetName()

	// the SMTP envelope FromAddr and the From header should match: if they
	// don't, Gmail tends to send to spam and Outlook rewrites the from address
	// to something really odd
	fromAddr := NOTIFY_EMAIL
	from := fmt.Sprintf("%s <%s>", fromName, fromAddr)
	// the Reply-To can use the actual commiter's email
	replyTo := fmt.Sprintf("%s <%s>", fromName, commit.GetAuthor().GetEmail())
	email := &EmailMsg{
		To:       to,
		From:     from,
		FromAddr: fromAddr,
		ReplyTo:  replyTo,
		Subject:  subject,
		Date:     time.Now().Format(time.RFC1123Z),
		Body:     body,
	}
	return email, nil
}
