package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type TestPush struct {
	Headers map[string]yaml.Node `yaml:"headers"`
	Payload string               `yaml:"payload"`
}

func parseTestPush(fileName string) (*TestPush, error) {
	file, err := os.Open(fileName)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var testPush TestPush
	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(&testPush); err != nil {
		return nil, fmt.Errorf("failed to decode yaml: %w", err)
	}

	return &testPush, nil
}

func main() {
	fileName := flag.String("file", "", "push yaml file")
	port := flag.String("port", "8000", "local port to make request to")
	flag.Parse()

	if *fileName == "" {
		log.Fatal("file name must be provided")
	}

	testPush, err := parseTestPush(*fileName)
	if err != nil {
		log.Fatal(err)
	}

	client := &http.Client{}
	url := fmt.Sprintf("http://localhost:%s/webhook", *port)
	req, err := http.NewRequest("POST", url, strings.NewReader(testPush.Payload))
	if err != nil {
		log.Fatal(err)
	}
	for key, val := range testPush.Headers {
		if key == "Request method" {
			continue
		}
		if key == "X-Hub-Signature" || key == "X-Hub-Signature-256" {
			continue
		}
		req.Header.Add(key, val.Value)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("%v\n%s\n", resp.Status, string(body))
}
