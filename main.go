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
	"strconv"
	"strings"
	"time"

	pc "github.com/gitgerby/stochasticparrot/internal/pkg/config"
)

// GiteaWebhookPayload represents the structure of Gitea webhook payload for pull requests
type GiteaWebhookPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		ID      int    `json:"id"`
		Number  int    `json:"number"`
		Title   string `json:"title"`
		DiffURL string `json:"diff_url"`
		Head    struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"base"`
		RequestedReviewers []GiteaReviewer `json:"requested_reviewers"`
		HTMLURL            string          `json:"html_url"`
	} `json:"pull_request"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		URL      string `json:"url"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
	RequestedReviewer GiteaReviewer `json:"requested_reviewer"`
}

type GiteaReviewer struct {
	Username string `json:"username"`
}

// OpenAIRequest represents the request to OpenAI-compatible endpoint
type OpenAIRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
}

// Message represents a message in OpenAI request
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenAIResponse represents the response from OpenAI-compatible endpoint
type OpenAIResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

// GiteaComment represents a comment to be posted on Gitea
type GiteaComment struct {
	Body        string `json:"body"`
	ReviewState string `json:"event"`
}

var config pc.ParrotConfig

var (
	debugConfig = flag.Bool("debug", false, "Enable debug mode")
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

	if *config.GiteaToken == "" || *config.OpenAIToken == "" || *config.OpenAIEndpoint == "" {
		log.Fatal("Missing required configuration")
	}

	// Fetch diff from Gitea
	diff, err := fetchDiff(*config.GiteaToken, payload)
	if err != nil {
		log.Printf("Error fetching diff: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get review from API Endpoint
	log.Print("Requesting review from LLM")
	review, err := getReview(*config.OpenAIToken, *config.OpenAIEndpoint, diff)
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

func getReview(token, endpoint, diff string) (string, error) {
	// Create prompt for OpenAI
	prompt := fmt.Sprintf(*config.ReviewPrompt, diff)

	// Prepare OpenAI request
	openaiReq := OpenAIRequest{
		Model: "gpt-4", // or your preferred model
		Messages: []Message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Temperature: 0.7,
		MaxTokens:   2000,
	}

	jsonData, err := json.Marshal(openaiReq)
	if err != nil {
		return "", err
	}

	// Send request to OpenAI-compatible endpoint

	// Log the JSON sent to the endpoint when debugging.
	// TODO(gearnsc): remove this.
	if *debugConfig {
		log.Printf("Debug LLM Request:\n%q", jsonData)
	}
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: time.Duration(*config.LLMTimeout) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OpenAI API error: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var openaiResp OpenAIResponse
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		return "", err
	}

	if len(openaiResp.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenAI")
	}

	return openaiResp.Choices[0].Message.Content, nil
}

func postComment(token string, payload GiteaWebhookPayload, review string) error {
	// Format comment
	commentBody := fmt.Sprintf(`## Automated Code Review

%s

*This review was automatically generated by the code review bot.*

`, review)

	comment := GiteaComment{
		Body:        commentBody,
		ReviewState: "APPROVED",
	}

	jsonData, err := json.Marshal(comment)
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
