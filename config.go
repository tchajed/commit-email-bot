package main

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// handling repo config (commit-emails.json)

type CommitEmailConfig struct {
	MailingList string `json:"mailingList"`
	EmailFormat string `json:"emailFormat,omitempty"`
}

type MissingConfigError struct{}

func (e MissingConfigError) Error() string {
	return "no commit-emails.json found"
}

func parseConfig(configText []byte) (config CommitEmailConfig, err error) {
	dec := json.NewDecoder(bytes.NewReader(configText))
	dec.DisallowUnknownFields()
	err = dec.Decode(&config)
	if err != nil {
		return CommitEmailConfig{}, fmt.Errorf("decoding commit-emails.json: %s", err)
	}
	if config.EmailFormat != "" {
		if !(config.EmailFormat == "html" || config.EmailFormat == "text") {
			return CommitEmailConfig{}, fmt.Errorf("invalid emailFormat (should be html or text): %q", config.EmailFormat)
		}
	}
	return
}

// getConfig reads the commit-emails.json file for a git repo
func getConfig(gitRepo string) (config CommitEmailConfig, err error) {
	configText, err := runGitCmd(gitRepo, "show", "HEAD:.github/commit-emails.json")
	if err != nil {
		return CommitEmailConfig{}, MissingConfigError{}
	}
	return parseConfig(configText)
}
