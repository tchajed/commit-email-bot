package main

import (
	"fmt"
	"github.com/BurntSushi/toml"
	"log/slog"
)

// handling repo config (commit-emails.toml)

type CommitEmailConfig struct {
	MailingList string `toml:"to"`
	Email       struct {
		Format string `toml:"format"`
	}
}

type MissingConfigError struct{}

func (e MissingConfigError) Error() string {
	return "no commit-emails.toml found"
}

func parseConfig(configText []byte) (config CommitEmailConfig, err error) {
	meta, err := toml.Decode(string(configText), &config)
	if err != nil {
		return CommitEmailConfig{}, fmt.Errorf("decoding commit-emails.toml: %s", err)
	}
	if len(meta.Undecoded()) > 0 {
		slog.Warn("unknown config fields: %v", meta.Undecoded())
	}
	format := config.Email.Format
	if !(format == "" || format == "html" || format == "text") {
		return CommitEmailConfig{}, fmt.Errorf("invalid email.format (should be html or text): %s", format)
	}
	return
}

// getConfig reads the commit-emails.toml file for a git repo
func getConfig(gitRepo string) (config CommitEmailConfig, err error) {
	configText, err := GitShow(gitRepo, "HEAD", ".github/commit-emails.toml")
	if err != nil {
		return CommitEmailConfig{}, MissingConfigError{}
	}
	return parseConfig(configText)
}
