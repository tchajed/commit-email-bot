package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/google/go-github/v75/github"
)

func coloredDiffToHtml(output string, commitURL string) (string, error) {
	// Parse the first line which has format "hash|message"
	firstLine, rest, _ := strings.Cut(output, "\n")
	hash, message, _ := strings.Cut(firstLine, "|")

	// Skip the "---" separator line
	_, diff, _ := strings.Cut(rest, "\n")

	// Convert the rest of the diff to HTML using aha to process ANSI colors
	ahaCmd := exec.Command("aha", "--no-header")
	ahaCmd.Stdin = strings.NewReader(diff)
	var stdout, stderr bytes.Buffer
	ahaCmd.Stdout, ahaCmd.Stderr = &stdout, &stderr
	err := ahaCmd.Run()
	if err != nil {
		return "", fmt.Errorf("error running aha: %w", err)
	}

	// Create the first part of the email with the
	table := fmt.Sprintf(`<table cellpadding="5" align="left">
<tr><td><a href="%s">%s</a></td>
<td>%s</td></tr>
</table>`, commitURL, hash, message)
	emailHtml := fmt.Sprintf(`<pre>
%s
%s
</pre>`, table, stdout.String())

	return emailHtml, nil
}

func gitDiffHtml(gitDir string, commitId string, commitURL string) (string, error) {
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

	// Capture delta output into a buffer
	var deltaOutput bytes.Buffer
	deltaCmd.Stdout = &deltaOutput

	// Start commands
	if err := gitCmd.Start(); err != nil {
		return "", fmt.Errorf("error starting git: %w", err)
	}
	if err := deltaCmd.Start(); err != nil {
		return "", fmt.Errorf("error starting delta: %w", err)
	}

	// Wait for commands to complete
	if err := gitCmd.Wait(); err != nil {
		return "", fmt.Errorf("git show failed: %w", err)
	}
	if err := deltaCmd.Wait(); err != nil {
		return "", fmt.Errorf("delta failed: %w", err)
	}

	return coloredDiffToHtml(deltaOutput.String(), commitURL)
}

func commitToEmail(gitDir string, repo string, branch string, commit *github.HeadCommit) (*EmailMsg, error) {
	config, err := getConfig(gitDir)
	if err != nil {
		return nil, fmt.Errorf("could not get config for %s: %w", repo, err)
	}
	to := config.MailingList
	body, err := gitDiffHtml(gitDir, commit.GetID(), commit.GetURL())
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
