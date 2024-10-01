package main

import (
	"bytes"
	"context"
	"crypto/tls"
	_ "embed"
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
	"syscall"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v62/github"
	"golang.org/x/crypto/acme/autocert"
)

var hostname = flag.String("hostname", "", "tls hostname (use localhost to disable https)")
var persistPath = flag.String("persist", "persist", "directory for persistent data")
var port = flag.String("port", "https", "port to listen on")

//go:embed index.html
var indexHTML []byte

// read from $WEBHOOK_SECRET
var webhookSecret []byte

// read from $MAIL_SMTP_PASSWORD
var smtpPassword string

// read from $GITHUB_APP_ID
var appId int64

// read from $GITHUB_APP_PRIVATE_KEY
var appPrivateKey []byte

var errorLog *log.Logger

func main() {
	flag.Parse()
	if *hostname == "" {
		*hostname = os.Getenv("TLS_HOSTNAME")
	}
	if *hostname == "" {
		log.Fatal("please set -hostname or $TLS_HOSTNAME")
	}
	if *hostname == "localhost" {
		if *port == "https" {
			log.Fatal("https on localhost will not work (choose another port)")
		}
	}
	smtpPassword = os.Getenv("MAIL_SMTP_PASSWORD")
	if smtpPassword == "" {
		log.Printf("no MAIL_SMTP_PASSWORD set, will print to stdout")
	}
	appPrivateKey = []byte(os.Getenv("GITHUB_APP_PRIVATE_KEY"))
	appIdStr := os.Getenv("GITHUB_APP_ID")
	if appIdStr != "" {
		var err error
		appId, err = strconv.ParseInt(appIdStr, 10, 64)
		if err != nil {
			log.Fatalf("invalid GITHUB_APP_ID: %v", err)
		}
	}

	secret := os.Getenv("WEBHOOK_SECRET")
	if secret == "" {
		log.Fatal("$WEBHOOK_SECRET is not set")
	}
	webhookSecret = []byte(secret)

	if err := os.MkdirAll(*persistPath, 0770); err != nil {
		log.Fatal(err)
	}
	errorLogPath := filepath.Join(*persistPath, "errors.log")
	errorFile, err := os.OpenFile(errorLogPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0660)
	if err != nil {
		log.Fatal(err)
	}
	defer errorFile.Close()
	errorLog = log.New(errorFile, "", log.LstdFlags|log.LUTC|log.Lshortfile)

	tlsKeysDir := filepath.Join(*persistPath, "tls_keys")
	certManager := autocert.Manager{
		Cache:      autocert.DirCache(tlsKeysDir),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(*hostname, fmt.Sprintf("www.%s", *hostname)),
	}
	// This HTTP handler listens for ACME "http-01" challenges, and redirects
	// other requests. It's useful for the latter in production in case someone
	// navigates to the website without https.
	//
	// On localhost this makes no sense to run.
	if *hostname != "localhost" {
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
	mux.HandleFunc("/webhook", githubEventHandler)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%s", *port),
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

	fmt.Printf("listening on :%s\n", *port)
	if *hostname == "localhost" {
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

func githubEventHandler(w http.ResponseWriter, req *http.Request) {
	payload, err := github.ValidatePayload(req, webhookSecret)
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
		err := githubPushHandler(context.TODO(), event)
		if err != nil {
			err = fmt.Errorf("push handler failed: %s", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			log.Println(err)
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

func githubPushHandler(ctx context.Context, ev *github.PushEvent) error {
	itr, err := ghinstallation.New(http.DefaultTransport, appId, *ev.Installation.ID, appPrivateKey)
	if err != nil {
		return err
	}
	token, err := itr.Token(ctx)
	if err != nil {
		return err
	}
	log.Printf("token: %s", token) // TODO: debugging only
	gitDir := filepath.Join(*persistPath, "repos", "github.com", *ev.Repo.FullName)

	if err := syncRepo(gitDir, *ev.Repo.CloneURL); err != nil {
		return err
	}

	log.Printf("git_multimail_wrapper.py %s %s %s", *ev.Before, *ev.After, *ev.Ref)
	args := []string{}
	if smtpPassword == "" {
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
	cmd.Env = append(cmd.Env, "GIT_CONFIG_PARAMETERS="+fmt.Sprintf("'multimailhook.smtpPass=%s'", smtpPassword))
	_, err = cmd.Output()
	if err == nil {
		return nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return fmt.Errorf("git_multimail_wrapper.py failed: %s:\n%s", ee.ProcessState.String(), ee.Stderr)
	}
	return err
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
