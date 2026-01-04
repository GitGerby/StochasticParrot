package main

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

// LLMRequest represents the request to OpenAI-compatible endpoint
type LLMRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
	Format      LLMFormat `json:"format"`
}

type LLMFormat struct {
	FormatType string `json:"type"`
	Strict     bool   `json:"strict"`
	Schema     string `json:"schema"`
}

// Message represents a message in OpenAI request
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LLMResponse represents the response from OpenAI-compatible endpoint
type LLMResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

// GiteaReviewResponse represents a review to be posted on Gitea
type GiteaReviewResponse struct {
	Body        string         `json:"body"`
	Comments    []GiteaComment `json:"comments"`
	ReviewState string         `json:"event"`
}

// GiteaComment represents a line specfic comment in a review
type GiteaComment struct {
	Body               string `json:"body"`
	CommentPositionNew int    `json:"new_position"`
	CommentPositionOld int    `json:"old_position"`
	Path               string `json:"path"`
}

type GiteaPRDetails struct {
	Title       string `json:"title"`
	Description string `json:"body"`
	Diff        string
}

type queuedRequest struct {
	prDetails *GiteaPRDetails
	payload   GiteaWebhookPayload
}
