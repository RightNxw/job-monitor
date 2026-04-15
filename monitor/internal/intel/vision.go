package intel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OCRImageVision sends an image to Haiku vision to extract text and links.
// Used as fallback when Tesseract OCR produces junk.
func (p *Parser) OCRImageVision(ctx context.Context, imageURL string) (*OCRResult, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("no API key")
	}

	// Download the image
	imgReq, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return nil, err
	}
	imgResp, err := (&http.Client{Timeout: 10 * time.Second}).Do(imgReq)
	if err != nil {
		return nil, fmt.Errorf("download image: %w", err)
	}
	defer imgResp.Body.Close()

	imgData, err := io.ReadAll(imgResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}

	b64 := base64.StdEncoding.EncodeToString(imgData)
	mediaType := imgResp.Header.Get("Content-Type")
	if mediaType == "" {
		mediaType = "image/jpeg"
	}

	reqBody := map[string]any{
		"model":      p.model,
		"max_tokens": 500,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "image",
						"source": map[string]string{
							"type":       "base64",
							"media_type": mediaType,
							"data":       b64,
						},
					},
					{
						"type": "text",
						"text": "Extract ALL text visible in this image. Also extract any URLs or application links. Return JSON: {\"text\": \"full extracted text\", \"links\": [\"url1\", \"url2\"]}. Return ONLY valid JSON, no markdown.",
					},
				},
			},
		},
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", p.apiKey)
	req.Header.Set("Anthropic-Version", "2023-06-01")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("anthropic API %d: %s", resp.StatusCode, string(respBody[:min(len(respBody), 300)]))
	}

	var apiResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, err
	}

	if len(apiResp.Content) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	// Strip markdown fences
	rawText := apiResp.Content[0].Text
	if len(rawText) > 3 && rawText[:3] == "```" {
		lines := splitLines(rawText)
		if len(lines) >= 3 {
			rawText = joinLines(lines[1 : len(lines)-1])
		}
	}

	var result OCRResult
	if err := json.Unmarshal([]byte(rawText), &result); err != nil {
		// If JSON parse fails, treat whole response as text
		result.Text = apiResp.Content[0].Text
		result.Links = extractURLs(result.Text)
	}

	return &result, nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func joinLines(lines []string) string {
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return result
}
