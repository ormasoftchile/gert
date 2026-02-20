// Package compiler provides LLM client interfaces and Azure OpenAI integration
// for Stage B of the TSG-to-runbook compilation pipeline.
package compiler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// LLMClient defines the interface for LLM-backed prose-to-schema conversion.
type LLMClient interface {
	// Complete sends a system prompt and user prompt to the LLM and returns
	// the assistant's response text.
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)

	// ModelName returns the deployment/model name for provenance tracking.
	ModelName() string
}

// AzureOpenAIClient implements LLMClient using the Azure OpenAI REST API.
type AzureOpenAIClient struct {
	Endpoint   string // e.g. https://<resource>.openai.azure.com
	APIKey     string
	Deployment string // model deployment name
	APIVersion string // e.g. 2024-02-01
	HTTPClient *http.Client
}

// AzureOpenAIConfig holds configuration for creating an Azure OpenAI client.
type AzureOpenAIConfig struct {
	Endpoint   string
	APIKey     string
	Deployment string
	APIVersion string
}

// NewAzureOpenAIClient creates a client from explicit config.
func NewAzureOpenAIClient(cfg AzureOpenAIConfig) (*AzureOpenAIClient, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("AZURE_OPENAI_ENDPOINT is required")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("AZURE_OPENAI_API_KEY is required")
	}
	if cfg.Deployment == "" {
		return nil, fmt.Errorf("AZURE_OPENAI_DEPLOYMENT is required")
	}
	if cfg.APIVersion == "" {
		cfg.APIVersion = "2024-02-01"
	}
	return &AzureOpenAIClient{
		Endpoint:   strings.TrimRight(cfg.Endpoint, "/"),
		APIKey:     cfg.APIKey,
		Deployment: cfg.Deployment,
		APIVersion: cfg.APIVersion,
		HTTPClient: &http.Client{Timeout: 120 * time.Second},
	}, nil
}

// NewAzureOpenAIClientFromEnv creates a client from environment variables:
//
//	AZURE_OPENAI_ENDPOINT   – required
//	AZURE_OPENAI_API_KEY    – required
//	AZURE_OPENAI_DEPLOYMENT – required
//	AZURE_OPENAI_API_VERSION – optional (default "2024-02-01")
func NewAzureOpenAIClientFromEnv() (*AzureOpenAIClient, error) {
	return NewAzureOpenAIClient(AzureOpenAIConfig{
		Endpoint:   os.Getenv("AZURE_OPENAI_ENDPOINT"),
		APIKey:     os.Getenv("AZURE_OPENAI_API_KEY"),
		Deployment: os.Getenv("AZURE_OPENAI_DEPLOYMENT"),
		APIVersion: envOrDefault("AZURE_OPENAI_API_VERSION", "2024-02-01"),
	})
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// chatRequest is the Azure OpenAI chat completions request body.
type chatRequest struct {
	Messages            []chatMessage `json:"messages"`
	Temperature         float64       `json:"temperature,omitempty"`
	MaxCompletionTokens int           `json:"max_completion_tokens,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatResponse is the Azure OpenAI chat completions response body.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

// ModelName returns the deployment name for provenance tracking.
func (c *AzureOpenAIClient) ModelName() string {
	return c.Deployment
}

// Complete sends a chat completion request to Azure OpenAI.
func (c *AzureOpenAIClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	url := fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
		c.Endpoint, c.Deployment, c.APIVersion)

	reqBody := chatRequest{
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		MaxCompletionTokens: 16384,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Azure OpenAI returned %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("API error [%s]: %s", chatResp.Error.Code, chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	if chatResp.Choices[0].FinishReason == "length" {
		return "", fmt.Errorf("LLM response was truncated (hit max_completion_tokens). Try a smaller TSG or increase the token limit")
	}

	return chatResp.Choices[0].Message.Content, nil
}

// Response delimiters used to parse LLM output into runbook + mapping.
const (
	RunbookStartMarker = "---RUNBOOK_YAML---"
	RunbookEndMarker   = "---END_RUNBOOK_YAML---"
	MappingStartMarker = "---MAPPING_MD---"
	MappingEndMarker   = "---END_MAPPING_MD---"
)

// ParseLLMResponse extracts runbook YAML and mapping markdown from the
// LLM's structured response using the delimiter markers.
func ParseLLMResponse(response string) (runbookYAML string, mappingMD string, err error) {
	// Strip any code fence wrapper the LLM might have added
	response = stripOuterCodeFence(response)

	runbookYAML, err = extractBetween(response, RunbookStartMarker, RunbookEndMarker)
	if err != nil {
		return "", "", fmt.Errorf("extract runbook: %w", err)
	}

	// For mapping, tolerate a missing end marker since it's the last section.
	mappingMD, err = extractBetweenTolerant(response, MappingStartMarker, MappingEndMarker)
	if err != nil {
		return "", "", fmt.Errorf("extract mapping: %w", err)
	}

	return strings.TrimSpace(runbookYAML), strings.TrimSpace(mappingMD), nil
}

// stripOuterCodeFence removes a wrapping ```...``` code fence if present.
func stripOuterCodeFence(s string) string {
	trimmed := strings.TrimSpace(s)
	if strings.HasPrefix(trimmed, "```") {
		// Find end of opening fence line
		if idx := strings.Index(trimmed, "\n"); idx != -1 {
			trimmed = trimmed[idx+1:]
		}
		// Remove closing fence
		if last := strings.LastIndex(trimmed, "```"); last != -1 {
			trimmed = trimmed[:last]
		}
	}
	return trimmed
}

func extractBetween(s, startMarker, endMarker string) (string, error) {
	startIdx := strings.Index(s, startMarker)
	if startIdx == -1 {
		return "", fmt.Errorf("missing %q marker in LLM response", startMarker)
	}
	contentStart := startIdx + len(startMarker)

	endIdx := strings.Index(s[contentStart:], endMarker)
	if endIdx == -1 {
		return "", fmt.Errorf("missing %q marker in LLM response", endMarker)
	}

	return s[contentStart : contentStart+endIdx], nil
}

// extractBetweenTolerant is like extractBetween but if the end marker is
// missing, returns everything after the start marker (useful for the last
// section which may get truncated).
func extractBetweenTolerant(s, startMarker, endMarker string) (string, error) {
	startIdx := strings.Index(s, startMarker)
	if startIdx == -1 {
		return "", fmt.Errorf("missing %q marker in LLM response", startMarker)
	}
	contentStart := startIdx + len(startMarker)

	endIdx := strings.Index(s[contentStart:], endMarker)
	if endIdx == -1 {
		// Tolerate missing end marker — take everything remaining
		return strings.TrimSpace(s[contentStart:]), nil
	}

	return s[contentStart : contentStart+endIdx], nil
}
