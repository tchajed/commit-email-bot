package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

var hostname = flag.String("hostname", "", "tls hostname")
var persistPath = flag.String("persist", "persist", "directory for persistent data")
var port = flag.String("port", "https", "port to listen on")

//go:embed index.html
var indexHTML string
var webhookSecret string

func main() {
	flag.Parse()
	if *hostname == "" {
		*hostname = os.Getenv("TLS_HOSTNAME")
	}
	if *hostname == "" {
		log.Fatal("please set -hostname or $TLS_HOSTNAME")
	}

	if err := os.MkdirAll(*persistPath, 0770); err != nil {
		log.Fatal(err)
	}

	webhookSecret = os.Getenv("WEBHOOK_SECRET")
	if webhookSecret == "" {
		log.Fatal("$WEBHOOK_SECRET is not set")
	}

	errorLogPath := filepath.Join(*persistPath, "errors.log")
	errorFile, err := os.OpenFile(errorLogPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0660)
	if err != nil {
		log.Fatal(err)
	}
	defer errorFile.Close()
	errorLog := log.New(errorFile, "", log.LstdFlags|log.LUTC|log.Lshortfile)

	tlsKeysDir := filepath.Join(*persistPath, "tls_keys")
	certManager := autocert.Manager{
		Cache:      autocert.DirCache(tlsKeysDir),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(*hostname),
	}
	go func() {
		err := http.ListenAndServe(":http", certManager.HTTPHandler(nil))
		if err != nil {
			log.Fatalf("http.ListenAndServe: %s", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(indexHTML))
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
	err = httpServer.ListenAndServeTLS("", "")
	if err != nil {
		log.Printf("http listen: %s", err)
	}

	<-shutdownDone
}

type GithubEvent interface {
	GetRepository() GithubRepo
	GetSender() GithubSender
}

type GithubGenericEvent struct {
	Repository GithubRepo
	Sender     GithubSender
}

func (e GithubGenericEvent) GetRepository() GithubRepo { return e.Repository }
func (e GithubGenericEvent) GetSender() GithubSender   { return e.Sender }

type GithubRepo struct {
	FullName string `json:"full_name"`
	CloneUrl string `json:"clone_url"`
	Private  bool
}

type GithubSender struct {
	Login string
}

type GithubPushEvent struct {
	*GithubGenericEvent

	Ref    string
	Before string
	After  string

	Pusher struct {
		Name  string
		Email string
	}
}

func githubEventHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		http.Error(w, "unexpected method: "+req.Method, http.StatusBadRequest)
		return
	}

	sig := req.Header.Get("X-Hub-Signature-256")
	var expectedHash []byte
	if n, err := fmt.Sscanf(sig, "sha256=%x", &expectedHash); n != 1 {
		http.Error(w, "invalid signature: "+err.Error(), http.StatusBadRequest)
		return
	}

	body := http.MaxBytesReader(w, req.Body, 1024*1024)
	payload, _ := io.ReadAll(body)

	h := hmac.New(sha256.New, []byte(webhookSecret))
	h.Write(payload)
	actualHash := h.Sum(nil)
	if !hmac.Equal(actualHash, expectedHash) {
		http.Error(w, "signature verification failed", http.StatusBadRequest)
		return
	}

	eventType := req.Header.Get("X-GitHub-Event")

	var payloadData interface{}
	switch eventType {
	case "":
		http.Error(w, "no event type specified", http.StatusBadRequest)
		return
	case "push":
		payloadData = new(GithubPushEvent)
	default:
		payloadData = new(GithubGenericEvent)
	}

	if err := json.Unmarshal(payload, payloadData); err != nil {
		http.Error(w, "failed to parse payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	event := payloadData.(GithubEvent)
	log.Printf("%s: %s from %s", event.GetRepository().FullName, eventType, event.GetSender().Login)

	if eventType == "ping" {
		url := event.GetRepository().CloneUrl
		gitDir := filepath.Join(*persistPath, "repos", "github.com", event.GetRepository().FullName)

		err := syncRepo(gitDir, url)
		if err != nil {
			err = fmt.Errorf("syncing repo %q failed: %s", url, err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			log.Println(err)
			return
		}

		_, _ = w.Write([]byte("Pong"))
		return
	}

	if eventType == "push" {
		pushEvent := payloadData.(*GithubPushEvent)
		err := githubPushHandler(pushEvent)
		if err != nil {
			err = fmt.Errorf("push handler failed: %s", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			log.Println(err)
			return
		}
		_, _ = w.Write([]byte("OK"))
		log.Printf("%s: push success: %s %s -> %s", pushEvent.Repository.FullName, pushEvent.Ref, pushEvent.Before[:8], pushEvent.After[:8])
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

func githubPushHandler(ev *GithubPushEvent) error {
	gitDir := filepath.Join(*persistPath, "repos", "github.com", ev.Repository.FullName)

	if err := syncRepo(gitDir, ev.Repository.CloneUrl); err != nil {
		return err
	}

	log.Printf("post-receive.py %s %s %s", ev.Before, ev.After, ev.Ref)
	cmd := exec.Command("./post-receive.py", "--debug") // --debug for debugging
	stdin := bytes.NewReader([]byte(fmt.Sprintf("%s %s %s", ev.Before, ev.After, ev.Ref)))
	cmd.Stdin = stdin
	cmd.Env = append(os.Environ(), "GIT_DIR="+gitDir)
	out, err := cmd.Output()
	if err == nil {
		fmt.Println(string(out)) // debugging
		return nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return fmt.Errorf("post-receive.py failed: %s:\n%s", ee.ProcessState.String(), ee.Stderr)
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
