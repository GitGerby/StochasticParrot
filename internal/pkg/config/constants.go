package config

const (
	defaultReviewPrompt = `You are an expert code reviewer. Analyze this git diff and provide a detailed review:

%s

Provide your review in the following format:
- Code quality assessment
- Potential issues or improvements
- Specific suggestions for each file

If there are no issues, just say "No issues found".`

	defaultPort       = 8080
	defaultLLMTimeout = 90
)
