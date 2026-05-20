package llm

// NewGoogleProvider builds a Provider for Google Gemini via its
// OpenAI-compatible API endpoint.
func NewGoogleProvider(apiKey string) Provider {
	return NewOpenAIProvider("google", "https://generativelanguage.googleapis.com/v1beta/openai/", apiKey)
}
