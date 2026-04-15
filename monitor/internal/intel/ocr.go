package intel

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// OCRResult is the extracted text and links from a story image.
type OCRResult struct {
	Text  string   `json:"text"`
	Links []string `json:"links"`
}

// OCRImage downloads an image and runs Tesseract OCR locally.
// Returns the extracted text and any URLs found in it.
// No API calls, runs entirely on the local machine.
func OCRImage(ctx context.Context, imageURL string) (*OCRResult, error) {
	// Download the image to a temp file
	imgReq, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return nil, err
	}
	imgResp, err := (&http.Client{Timeout: 10 * time.Second}).Do(imgReq)
	if err != nil {
		return nil, fmt.Errorf("download image: %w", err)
	}
	defer imgResp.Body.Close()

	// Use current directory for temp file, tesseract can have issues with /tmp/ paths
	tmpFile, err := os.CreateTemp(".", ".ocr-*.jpg")
	if err != nil {
		// Fall back to system temp
		tmpFile, err = os.CreateTemp("", "ocr-*.jpg")
		if err != nil {
			return nil, fmt.Errorf("create temp: %w", err)
		}
	}
	defer os.Remove(tmpFile.Name())

	if _, err := io.Copy(tmpFile, imgResp.Body); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("save image: %w", err)
	}
	tmpFile.Close()

	// Run tesseract
	cmd := exec.CommandContext(ctx, "tesseract", tmpFile.Name(), "stdout", "--psm", "6")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tesseract: %w", err)
	}

	text := strings.TrimSpace(string(output))
	links := extractURLs(text)

	return &OCRResult{
		Text:  text,
		Links: links,
	}, nil
}

var urlRegex = regexp.MustCompile(`https?://[^\s"'\])<>]+`)

func extractURLs(text string) []string {
	matches := urlRegex.FindAllString(text, -1)
	seen := make(map[string]bool)
	var unique []string
	for _, m := range matches {
		if !seen[m] {
			seen[m] = true
			unique = append(unique, m)
		}
	}
	return unique
}
