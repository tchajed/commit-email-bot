package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	_ "embed"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tchajed/commit-emails-bot/stats"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v62/github"
	"github.com/gregjones/httpcache"
	"golang.org/x/crypto/acme/autocert"
)

type AppConfig struct {
	Hostname    string
	PersistPath string
	Port        string

	EmailStdout   bool
	WebhookSecret []byte
	SmtpPassword  string
	AppId         int64
	AppPrivateKey []byte

	DenyAccounts map[string]bool
}

var Cfg AppConfig

func (cfg AppConfig) Denied(account string) bool {
	return cfg.DenyAccounts[account]
}

func init() {
	// If dotenvx is not used, an environment variable might still be encrypted.
	// Treat this as if the environment variable wasn't passed.
	getEncryptedEnv := func(varName string) string {
		raw := os.Getenv(varName)
		if strings.HasPrefix(raw, "encrypted:") {
			return ""
		}
		return raw
	}

	Cfg = AppConfig{}

	Cfg.Hostname = os.Getenv("TLS_HOSTNAME")
	if Cfg.Hostname == "" {
		Cfg.Hostname = "localhost"
	}
	Cfg.PersistPath = os.Getenv("PERSIST_PATH")
	if Cfg.PersistPath == "" {
		Cfg.PersistPath = "persist"
	}
	Cfg.Port = "https"
	Cfg.WebhookSecret = []byte(getEncryptedEnv("WEBHOOK_SECRET"))
	Cfg.SmtpPassword = getEncryptedEnv("MAIL_SMTP_PASSWORD")
	emailStdout := os.Getenv("EMAIL_STDOUT")
	if emailStdout == "true" || emailStdout == "1" {
		Cfg.EmailStdout = true
	}

	var err error
	appIdStr := getEncryptedEnv("GITHUB_APP_ID")
	if appIdStr != "" {
		Cfg.AppId, err = strconv.ParseInt(appIdStr, 10, 64)
		if err != nil {
			log.Fatalf("GITHUB_APP_ID is not a number, got %s", appIdStr)
		}
	}

	keyEncoded := getEncryptedEnv("GITHUB_APP_PRIVATE_KEY")
	if keyEncoded != "" {
		// base64 decode
		Cfg.AppPrivateKey, err = base64.StdEncoding.DecodeString(keyEncoded)
		if err != nil {
			log.Fatal("private key has invalid base64")
		}
	}
}

func (c AppConfig) Insecure() bool {
	return c.Hostname == "localhost"
}

//go:embed index.html
var indexHTML []byte

// Server tracks state for the running in-memory server
type Server struct {
	transport http.RoundTripper
	db        stats.Database
}

// PushHandler tracks state for a single push handler
type PushHandler struct {
	srv          Server
	installation int64
	repo         string
}

func openDenyAccounts(path string) map[string]bool {
	f, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0644)
	if err != nil {
		log.Fatalf("could not open deny file: %v", err)
	}
	deny := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		deny[scanner.Text()] = true
	}
	defer f.Close()
	return deny
}

func main() {
	flag.StringVar(&Cfg.Hostname, "hostname", Cfg.Hostname, "tls hostname (use localhost to disable https)")
	flag.StringVar(&Cfg.PersistPath, "persist", Cfg.PersistPath, "directory for persistent data")
	flag.StringVar(&Cfg.Port, "port", Cfg.Port, "port to listen on")
	flag.Parse()

	if Cfg.EmailStdout {
		Cfg.SmtpPassword = ""
	}

	if err := os.MkdirAll(Cfg.PersistPath, 0770); err != nil {
		log.Fatal(err)
	}

	Cfg.DenyAccounts = openDenyAccounts(filepath.Join(Cfg.PersistPath, "deny-accounts.txt"))

	logFile, err := os.OpenFile(
		filepath.Join(Cfg.PersistPath, "commit-email-bot.log"),
		os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0660)
	if err != nil {
		log.Fatalf("could not create log file: %v", err)
	}
	defer logFile.Close()
	handler := slog.NewJSONHandler(logFile, nil)
	logger := slog.New(handler)
	slog.SetDefault(logger)

	tlsKeysDir := filepath.Join(Cfg.PersistPath, "tls_keys")
	certManager := autocert.Manager{
		Cache:      autocert.DirCache(tlsKeysDir),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(Cfg.Hostname, fmt.Sprintf("www.%s", Cfg.Hostname)),
	}
	// This HTTP handler listens for ACME "http-01" challenges, and redirects
	// other requests. It's useful for the latter in production in case someone
	// navigates to the website without https.
	//
	// On localhost this makes no sense to run.
	if Cfg.Insecure() {
		go func() {
			err := http.ListenAndServe(":http", certManager.HTTPHandler(nil))
			if err != nil {
				log.Fatalf("http.ListenAndServe: %s", err)
			}
		}()
	}

	ct := httpcache.NewMemoryCacheTransport()
	db, err := stats.New(Cfg.PersistPath)
	if err != nil {
		log.Fatalf("could not open database: %v", err)
	}
	srv := Server{
		transport: ct,
		db:        db,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "max-age=600")
		_, _ = w.Write(indexHTML)
	})
	mux.HandleFunc("/webhook", func(w http.ResponseWriter, req *http.Request) {
		srv.githubEventHandler(w, req)
	})

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%s", Cfg.Port),
		Handler: mux,

		TLSConfig: &tls.Config{GetCertificate: certManager.GetCertificate},

		ErrorLog: slog.NewLogLogger(
			handler.WithAttrs([]slog.Attr{slog.String("source", "http")}),
			slog.LevelError,
		),

		ReadTimeout:  10 * time.Second,
		WriteTimeout: 360 * time.Second,
		IdleTimeout:  360 * time.Second,
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	shutdownDone := make(chan struct{})
	go func() {
		<-sigChan
		fmt.Println("Shutting down...")
		slog.Info("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := httpServer.Shutdown(ctx)
		if err != nil {
			slog.Error("http server shutdown", slog.String("error", err.Error()))
		}
		close(shutdownDone)
	}()

	if Cfg.SmtpPassword == "" {
		fmt.Println("sending emails to stdout")
	}
	fmt.Printf("host %s listening on :%s\n", Cfg.Hostname, Cfg.Port)
	slog.Info("starting server")
	if Cfg.Insecure() {
		err = httpServer.ListenAndServe()
	} else {
		err = httpServer.ListenAndServeTLS("", "")
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Warn("http listen", slog.String("error", err.Error()))
	}

	<-shutdownDone
}

func (srv Server) githubEventHandler(w http.ResponseWriter, req *http.Request) {
	payload, err := github.ValidatePayload(req, Cfg.WebhookSecret)
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
		account := event.GetRepo().GetOwner().GetLogin()
		if account == "" {
			account = event.GetRepo().GetOrganization()
		}
		if Cfg.Denied(account) {
			slog.Info("denied push", slog.String("account", account))
			http.Error(w, "account denied", http.StatusForbidden)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		repo := event.GetRepo().GetFullName()
		err := PushHandler{
			srv:          srv,
			installation: event.GetInstallation().GetID(),
			repo:         repo,
		}.githubPushHandler(ctx, event)
		if err != nil {
			err = fmt.Errorf("push handler failed: %s", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			slog.Error("push handler",
				slog.String("error", err.Error()),
				slog.String("repo", repo))
			return
		}
		srv.db.AddPush(event)
		_, _ = w.Write([]byte("OK"))
		before := (*event.Before)[:8]
		after := (*event.After)[:8]
		slog.Info("push success",
			slog.String("repo", repo),
			slog.String("ref change", fmt.Sprintf("%s: %s -> %s", event.GetRef(), before, after)),
		)
	case *github.InstallationEvent:
		slog.Info("installation",
			slog.String("action", event.GetAction()),
			slog.String("account", event.GetInstallation().GetAccount().GetLogin()),
		)
		srv.db.AddInstallation(event)
	case *github.InstallationRepositoriesEvent:
		slog.Info("installation",
			slog.String("action", event.GetAction()),
			slog.String("account", event.GetInstallation().GetAccount().GetLogin()),
		)
		srv.db.UpdateInstallation(event)
	default:
	}
}

func (h PushHandler) githubPushHandler(ctx context.Context, ev *github.PushEvent) error {
	itr, err := ghinstallation.New(h.srv.transport, Cfg.AppId, *ev.Installation.ID, Cfg.AppPrivateKey)
	if err != nil {
		return err
	}
	client := github.NewClient(&http.Client{Transport: itr})
	gitDir, err := SyncRepo(ctx, client, ev.Repo)
	if err != nil {
		if _, ok := err.(MissingConfigError); ok {
			slog.Info("push to unconfigured repo", slog.String("repo", h.repo))
			return nil
		}
		return err
	}

	args := []string{}
	if Cfg.SmtpPassword == "" {
		args = append(args, "--stdout")
	}
	config, err := getConfig(gitDir)
	if err != nil {
		return fmt.Errorf("could not get config for %s: %s", h.repo, err)
	}
	args = append(args, "-c", fmt.Sprintf("multimailhook.mailingList=%s", config.MailingList))
	if config.Email.Format != "" {
		args = append(args, "-c", fmt.Sprintf("multimailhook.commitEmailFormat=%s", config.Email.Format))
	}
	fromName := ev.GetHeadCommit().GetCommitter().GetName()
	fromAddress := "notifications@commit-emails.xyz"
	if fromName != "" {
		fromAddress = fmt.Sprintf("%s <%s>", fromName, fromAddress)
	}
	args = append(args, "-c", fmt.Sprintf("multimailhook.from=%s", fromAddress))
	args = append(args, "-c", fmt.Sprintf("multimailhook.commitBrowseURL=%s/commit/%%(id)s", ev.GetRepo().GetHTMLURL()))
	cmd := exec.Command("./git_multimail_wrapper.py", args...)
	commitLine := fmt.Sprintf("%s %s %s", *ev.Before, *ev.After, *ev.Ref)
	stdin := bytes.NewReader([]byte(commitLine))
	cmd.Stdin = stdin
	stderrBuf := &bytes.Buffer{}
	cmd.Stderr = stderrBuf
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "GIT_DIR="+gitDir)
	// constants that configure git_multimail
	cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL="+"git-multimail.config")
	// Provide the password via an environment variable - it cannot be in the
	// config file since that's public, and we don't want it to be in the command
	// line with -c since other processes can read that.
	//
	// Single quotes are necessary for git to parse this correctly.
	cmd.Env = append(cmd.Env, "GIT_CONFIG_PARAMETERS="+fmt.Sprintf("'multimailhook.smtpPass=%s'", Cfg.SmtpPassword))
	output, err := cmd.Output()
	if err == nil {
		return nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		slog.Error("git_multimail_wrapper.py failed",
			slog.String("push", commitLine),
			slog.String("stdout", string(output)),
			slog.String("stderr", stderrBuf.String()))
		return fmt.Errorf("git_multimail_wrapper.py  failed: %s", ee.ProcessState.String())
	}
	return err
}
