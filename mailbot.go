package main

import (
	"bytes"
	"context"
	"crypto/tls"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v62/github"
	"golang.org/x/crypto/acme/autocert"
)

type AppConfig struct {
	Hostname    string
	PersistPath string
	Port        string

	WebhookSecret []byte
	SmtpPassword  string
	AppId         int64
	AppPrivateKey []byte
}

// If dotenvx is not used, an environment variable might still be encrypted.
// Treat this as if the environment variable wasn't passed.
func getEncryptedEnv(varName string) string {
	raw := os.Getenv(varName)
	if strings.HasPrefix(raw, "encrypted:") {
		return ""
	}
	return raw
}

func NewAppConfig() AppConfig {
	config := AppConfig{}

	config.Hostname = os.Getenv("TLS_HOSTNAME")
	if config.Hostname == "" {
		config.Hostname = "localhost"
	}
	config.PersistPath = os.Getenv("PERSIST_PATH")
	if config.PersistPath == "" {
		config.PersistPath = "persist"
	}
	config.Port = "https"
	config.WebhookSecret = []byte(getEncryptedEnv("WEBHOOK_SECRET"))
	config.SmtpPassword = getEncryptedEnv("MAIL_SMTP_PASSWORD")

	var err error
	appIdStr := getEncryptedEnv("GITHUB_APP_ID")
	if appIdStr != "" {
		config.AppId, err = strconv.ParseInt(appIdStr, 10, 64)
		if err != nil {
			log.Fatalf("GITHUB_APP_ID is not a number, got %s", appIdStr)
		}
	}

	keyEncoded := getEncryptedEnv("GITHUB_APP_PRIVATE_KEY")
	if keyEncoded != "" {
		// base64 decode
		config.AppPrivateKey, err = base64.StdEncoding.DecodeString(keyEncoded)
		if err != nil {
			log.Fatal("private key has invalid base64")
		}
	}
	return config
}

func (c AppConfig) Insecure() bool {
	return c.Hostname == "localhost"
}

//go:embed index.html
var indexHTML []byte

var errorLog *log.Logger

func main() {
	config := NewAppConfig()
	flag.StringVar(&config.Hostname, "hostname", config.Hostname, "tls hostname (use localhost to disable https)")
	flag.StringVar(&config.PersistPath, "persist", config.PersistPath, "directory for persistent data")
	flag.StringVar(&config.Port, "port", config.Port, "port to listen on")
	flag.Parse()

	if err := os.MkdirAll(config.PersistPath, 0770); err != nil {
		log.Fatal(err)
	}
	errorLogPath := filepath.Join(config.PersistPath, "errors.log")
	errorFile, err := os.OpenFile(errorLogPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0660)
	if err != nil {
		log.Fatal(err)
	}
	defer errorFile.Close()
	errorLog = log.New(errorFile, "", log.LstdFlags|log.LUTC|log.Lshortfile)

	tlsKeysDir := filepath.Join(config.PersistPath, "tls_keys")
	certManager := autocert.Manager{
		Cache:      autocert.DirCache(tlsKeysDir),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(config.PersistPath, fmt.Sprintf("www.%s", config.Hostname)),
	}
	// This HTTP handler listens for ACME "http-01" challenges, and redirects
	// other requests. It's useful for the latter in production in case someone
	// navigates to the website without https.
	//
	// On localhost this makes no sense to run.
	if config.Insecure() {
		go func() {
			err := http.ListenAndServe(":http", certManager.HTTPHandler(nil))
			if err != nil {
				log.Fatalf("http.ListenAndServe: %s", err)
			}
		}()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write(indexHTML)
	})
	mux.HandleFunc("/webhook", func(w http.ResponseWriter, req *http.Request) {
		githubEventHandler(config, w, req)
	})

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%s", config.Port),
		Handler: mux,

		TLSConfig: &tls.Config{GetCertificate: certManager.GetCertificate},

		ErrorLog: errorLog,

		ReadTimeout:  10 * time.Second,
		WriteTimeout: 360 * time.Second,
		IdleTimeout:  360 * time.Second,
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	shutdownDone := make(chan struct{})
	go func() {
		<-sigChan
		log.Printf("Shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := httpServer.Shutdown(ctx)
		if err != nil {
			log.Printf("HTTP server shutdown with error: %s", err)
		}
		close(shutdownDone)
	}()

	fmt.Printf("host %s listening on :%s\n", config.Hostname, config.Port)
	if config.Insecure() {
		err = httpServer.ListenAndServe()
	} else {
		err = httpServer.ListenAndServeTLS("", "")
	}
	if err != nil {
		log.Printf("http listen: %s", err)
	}

	<-shutdownDone
}

type CommitEmailConfig struct {
	MailingList string `json:"mailingList"`
	EmailFormat string `json:"emailFormat,omitempty"`
}

// getConfig reads the commit-emails.json file for a git repo
func getConfig(gitRepo string) (config CommitEmailConfig, err error) {
	configText, err := runGitCmd(gitRepo, "show", "HEAD:.github/commit-emails.json")
	if err != nil {
		return
	}
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

func githubEventHandler(cfg AppConfig, w http.ResponseWriter, req *http.Request) {
	payload, err := github.ValidatePayload(req, cfg.WebhookSecret)
	if err != nil {
		http.Error(w, "could not validate payload: "+err.Error(), http.StatusBadRequest)
		return
	}
	event, err := github.ParseWebHook(github.WebHookType(req), payload)
	if err != nil {
		http.Error(w, "could not parse webhook: "+err.Error(), http.StatusBadRequest)
	}
	switch event := event.(type) {
	case *github.PingEvent:
		_, _ = w.Write([]byte("Pong"))
		return
	case *github.PushEvent:
		err := githubPushHandler(cfg, context.TODO(), event)
		if err != nil {
			err = fmt.Errorf("push handler failed: %s", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			errorLog.Println(err)
			return
		}
		_, _ = w.Write([]byte("OK"))
		log.Printf("%s: push success: %s %s -> %s", *event.Repo.FullName, *event.Ref, (*event.Before)[:8], (*event.After)[:8])
	case *github.InstallationEvent:
		log.Printf("installation %s by %s", *event.Action, *event.Installation.Account.Login)
		// TODO: check repositories we now have access to for commit-emails.json
	default:
	}
}

func githubPushHandler(cfg AppConfig, ctx context.Context, ev *github.PushEvent) error {
	itr, err := ghinstallation.New(http.DefaultTransport, cfg.AppId, *ev.Installation.ID, cfg.AppPrivateKey)
	if err != nil {
		return err
	}
	token, err := itr.Token(ctx)
	if err != nil {
		return err
	}
	log.Printf("token: %s", token) // TODO: debugging only
	gitDir := filepath.Join(cfg.PersistPath, "repos", "github.com", *ev.Repo.FullName)

	if err := syncRepo(gitDir, *ev.Repo.CloneURL); err != nil {
		return err
	}

	args := []string{}
	if cfg.SmtpPassword == "" {
		args = append(args, "--stdout")
	}
	config, err := getConfig(gitDir)
	if err != nil {
		log.Printf("no commit-emails.json found for %s: %s", *ev.Repo.FullName, err)
		return fmt.Errorf("no commit-emails.json found for %s: %s", *ev.Repo.FullName, err)
	}
	args = append(args, "-c", fmt.Sprintf("multimailhook.mailingList=%s", config.MailingList))
	if config.EmailFormat != "" {
		args = append(args, "-c", fmt.Sprintf("multimailhook.commitEmailFormat=%s", config.EmailFormat))
	}
	args = append(args, "-c", fmt.Sprintf("multimailhook.from=%s <notifications@commit-emails.xyz>", *ev.HeadCommit.Committer.Name))
	args = append(args, "-c", fmt.Sprintf("multimailhook.commitBrowseURL=%s/commit/%%(id)s", *ev.Repo.HTMLURL))
	cmd := exec.Command("./git_multimail_wrapper.py", args...)
	stdin := bytes.NewReader([]byte(fmt.Sprintf("%s %s %s", *ev.Before, *ev.After, *ev.Ref)))
	cmd.Stdin = stdin
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "GIT_DIR="+gitDir)
	// constants that configure git_multimail
	cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL="+"git-multimail.config")
	// Provide the password via an environment variable - it cannot be in the
	// config file since that's public, and we don't want it to be in the command
	// line with -c since other processes can read that.
	//
	// Single quotes are necessary for git to parse this correctly.
	cmd.Env = append(cmd.Env, "GIT_CONFIG_PARAMETERS="+fmt.Sprintf("'multimailhook.smtpPass=%s'", cfg.SmtpPassword))
	_, err = cmd.Output()
	if err == nil {
		return nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return fmt.Errorf("git_multimail_wrapper.py failed: %s:\n%s", ee.ProcessState.String(), ee.Stderr)
	}
	return err
}
