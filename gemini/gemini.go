// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

type part struct {
	Text string `json:"text"`
}

type content struct {
	Parts []part `json:"parts"`
	// Must be either 'user' or 'model'.
	Role string `json:"role,omitempty"`
}

type generateContentRequest struct {
	Contents []content `json:"contents"`
	/*
		// Range [0, 2]
		Temperature float32   `json:"temperature,omitzero"`
		TopP        float32   `json:"topP,omitzero"`
		TopK        int32     `json:"topK,omitzero"`
	*/
	/*
		Model:             m.fullName,
		Contents:          transformSlice(contents, (*Content).toProto),
		SafetySettings:    transformSlice(m.SafetySettings, (*SafetySetting).toProto),
		Tools:             transformSlice(m.Tools, (*Tool).toProto),
		ToolConfig:        m.ToolConfig.toProto(),
		GenerationConfig:  m.GenerationConfig.toProto(),
		SystemInstruction: m.SystemInstruction.toProto(),
		CachedContent:     cc,
	*/
}

type generateContentResponse struct {
	Candidates    []generateContentResponseCandidate   `json:"candidates"`
	UsageMetadata generateContentResponseUsageMetadata `json:"usageMetadata"`
	ModelVersion  string                               `json:"modelVersion"`
}
type generateContentResponseCandidate struct {
	Content      generateContentResponseCandidateContent `json:"content"`
	FinishReason string                                  `json:"finishReason"`
	AvgLogprobs  float64                                 `json:"avgLogprobs"`
}
type generateContentResponseCandidateContent struct {
	Parts []part `json:"parts"`
	Role  string `json:"role"`
}

type generateContentResponseUsageMetadata struct {
	PromptTokenCount        int                                                `json:"promptTokenCount"`
	CandidatesTokenCount    int                                                `json:"candidatesTokenCount"`
	TotalTokenCount         int                                                `json:"totalTokenCount"`
	PromptTokensDetails     []generateContentResponseUsageMetadataTokenDetails `json:"promptTokensDetails"`
	CandidatesTokensDetails []generateContentResponseUsageMetadataTokenDetails `json:"candidatesTokensDetails"`
}
type generateContentResponseUsageMetadataTokenDetails struct {
	Modality   string `json:"modality"`
	TokenCount int    `json:"tokenCount"`
}

// https://ai.google.dev/gemini-api/docs/pricing?hl=en
// https://pkg.go.dev/github.com/google/generative-ai-go/genai no need to use this package, it imports too much.
type Client struct {
	ApiKey string
	// https://ai.google.dev/gemini-api/docs/models/gemini?hl=en#gemini-2.0-flash
	//model := "gemini-2.0-flash"
	// https://ai.google.dev/gemini-api/docs/models/gemini?hl=en#gemini-2.0-flash-lite
	//model := "gemini-2.0-flash-lite"
	Model string
}

func (c *Client) Query(ctx context.Context, query string) (string, error) {
	// https://ai.google.dev/gemini-api/docs?hl=en#rest
	content := generateContentRequest{Contents: []content{{Parts: []part{{Text: query}}}}}
	data, err := json.Marshal(content)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://generativelanguage.googleapis.com/v1beta/models/"+c.Model+":generateContent?key="+c.ApiKey, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	if data, err = io.ReadAll(resp.Body); err != nil {
		return "", err
	}
	result := generateContentResponse{}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&result); err != nil {
		return "", fmt.Errorf("Failed to decode: %w\nOriginal data: %s", err, data)
	}
	if len(result.Candidates) != 1 {
		return "", fmt.Errorf("unexpected number of candidates; expected 1, got %v", result.Candidates)
	}
	parts := result.Candidates[0].Content.Parts
	response := parts[len(parts)-1].Text
	slog.Info("gemini", "query", query, "response", response, "stats", result.UsageMetadata)
	return response, nil
}
