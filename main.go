package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	pc "keemnun.somuchcrypto.com/gearnsc/llm-tooling/autoreview/internal/pkg/config"
)

var (
	config      pc.ParrotConfig
	debugConfig = flag.Bool("debug", false, "Enable debug mode")

	// some LLMs absolutely refuse to return a raw json object without wrapping it in markdown tags; make an attempt here to extract the encoded response.
	jsonRespRegex = regexp.MustCompile(`(?i)(?:^\x60\x60\x60(?:json)?)?[\s]*({[\s]*"body":[\w\W]*})(?:[\s]*\x60\x60\x60$)?`)
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to the configuration file")
	skipTLS := flag.Bool("InsecureSkipTLS", false, "Skip TLS verification on connections")
	flag.Parse()

	err := config.Parse(*configPath)
	if err != nil {
		panic(fmt.Sprintf("Error parsing config: %v", err))
	}

	if *config.InsecureSkipVerify || *skipTLS {
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	if *debugConfig {
		log.Printf("%+v", config)
	}

	http.HandleFunc("/webhook", handleWebhook)
	log.Printf("Server starting on port %d", *config.Port)
	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(*config.Port), nil))
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	var payload GiteaWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("Error unmarshaling webhook payload: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Only process review_requested events
	if !strings.EqualFold(payload.Action, "review_requested") {
		log.Printf("Ignoring action: %s", payload.Action)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Determine if bot user is in reviewer list
	isOurUserRequested := false

	// Check in PR scope for requested reviewers
	for _, reviewer := range payload.PullRequest.RequestedReviewers {
		if strings.EqualFold(reviewer.Username, *config.GiteaUsername) {
			isOurUserRequested = true
			break
		}
	}

	// Check if bot user is in requested reviewer list at top level scope
	if strings.EqualFold(payload.RequestedReviewer.Username, *config.GiteaUsername) {
		isOurUserRequested = true
	}

	// Only review PRs where we're specifically in the reviewer list OR we've
	// configured the GiteaUsername in the config file to '*'
	if !isOurUserRequested && *config.GiteaUsername != "*" {
		log.Printf("Our user: %q not in requested reviewers and gitea_username config option not set to '*'", *config.GiteaUsername)
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Printf("Processing PR #%d: %s", payload.PullRequest.Number, payload.PullRequest.Title)

	if *config.GiteaToken == "" || *config.LLMToken == "" || *config.LLMEndpoint == "" {
		log.Fatal("Missing required configuration")
	}

	// Fetch PR details from Gitea
	prDetails, err := fetchPRMeta(*config.GiteaToken, payload)
	if err != nil {
		log.Printf("Error fetching PR: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get review from API Endpoint
	log.Print("Requesting review from LLM")
	review, err := getReview(*config.LLMToken, *config.LLMEndpoint, prDetails)
	if err != nil {
		log.Printf("Error getting review: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Post comment to Gitea
	log.Print("Posting reply")
	if err := postComment(*config.GiteaToken, payload, review); err != nil {
		log.Printf("Error posting comment: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Successfully processed PR #%d", payload.PullRequest.Number)
}

func fetchDiff(token string, payload GiteaWebhookPayload) (string, error) {
	// Gitea API endpoint for pull request diff
	url := fmt.Sprintf("%s/pulls/%d.diff", payload.Repository.URL, payload.PullRequest.Number)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+token)

	log.Printf("Fetching diff from Gitea: %q", url)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch diff: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func fetchPRMeta(token string, payload GiteaWebhookPayload) (*GiteaPRDetails, error) {
	// Gitea API endpoint for pull request metadata
	url := fmt.Sprintf("%s/pulls/%d", payload.Repository.URL, payload.PullRequest.Number)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+token)

	log.Printf("Fetching PR metadata from Gitea: %q", url)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch diff: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var prd GiteaPRDetails
	if err := json.Unmarshal(body, &prd); err != nil {
		return nil, err
	}

	prd.Diff, err = fetchDiff(token, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch diff: %w", err)
	}

	return &prd, nil
}

func getReview(token, endpoint string, pullRequest *GiteaPRDetails) (*GiteaReviewResponse, error) {
	// Create embed PR details in the user role message for the LLM.
	prPrompt := fmt.Sprintf(*config.UserPrompt, pullRequest.Title, pullRequest.Description, pullRequest.Diff)

	// Prepare LLM request
	llmReq := LLMRequest{
		Model: *config.Model,
		Messages: []Message{
			{
				Role:    "system",
				Content: *config.SystemPrompt,
			},
			{
				Role:    "user",
				Content: prPrompt,
			},
		},
		Temperature: 0.7,
		MaxTokens:   2000,
		Format: LLMFormat{
			FormatType: "json_schema",
			Strict:     true,
			Schema: `{
  "body": "string",
  "comments": [
    {
      "body": "string",
      "new_position": 0,
      "path": "string"
    }
  ],
  "event": "string"
}`,
		},
	}

	jsonData, err := json.Marshal(llmReq)
	if err != nil {
		return nil, err
	}

	// Send request to OpenAI-compatible endpoint

	// Log the JSON sent to the endpoint when debugging.
	// TODO(gearnsc): remove this.
	if *debugConfig {
		log.Printf("Debug LLM Request:\n%q", jsonData)
	}
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: time.Duration(*config.LLMTimeout) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LLM API error: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var llmResp LLMResponse
	if err := json.Unmarshal(body, &llmResp); err != nil {
		return nil, err
	}

	if len(llmResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from LLM")
	}

	var reviewResponse GiteaReviewResponse
	if err := json.Unmarshal([]byte(llmResp.Choices[0].Message.Content), &reviewResponse); err != nil {
		log.Println("failed to unmarshall raw LLM response; attempting blob extraction")
		return llmRespCleanup(llmResp.Choices[0].Message.Content)
	}

	return &reviewResponse, nil
}

func llmRespCleanup(resp string) (*GiteaReviewResponse, error) {
	matches := jsonRespRegex.FindStringSubmatch(resp)
	if len(matches) < 2 {
		return nil, fmt.Errorf("failed to extract json from LLM response")
	}
	var rr GiteaReviewResponse
	if err := json.Unmarshal([]byte(matches[1]), &rr); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cleaned json: %v", err)
	}
	return &rr, nil
}

func postComment(token string, payload GiteaWebhookPayload, review *GiteaReviewResponse) error {
	// Format comment
	review.Body = fmt.Sprintf(`## Automated Code Review

%s

*This review was automatically generated by the code review bot.*

`, review.Body)

	jsonData, err := json.Marshal(review)
	if err != nil {
		return err
	}

	// Post comment to Gitea using the correct API endpoint
	url := fmt.Sprintf("%s/pulls/%d/reviews", payload.Repository.URL, payload.PullRequest.Number)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to post comment: %d - %s", resp.StatusCode, string(body))
	}

	return nil
}
