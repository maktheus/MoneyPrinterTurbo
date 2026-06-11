package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"nichebot/models"
	"os"
	"path/filepath"
	"time"
)

const uploadPostBase = "https://api.upload-post.com"

type UploadPostClient struct {
	apiKey       string
	username     string
	privacyLevel string
	httpClient   *http.Client
}

func NewUploadPostClient(cfg *models.UploadPostConfig) *UploadPostClient {
	return &UploadPostClient{
		apiKey:       cfg.APIKey,
		username:     cfg.Username,
		privacyLevel: cfg.PrivacyLevel,
		httpClient:   &http.Client{Timeout: 5 * time.Minute},
	}
}

type uploadResp struct {
	Success   bool   `json:"success"`
	RequestID string `json:"request_id"`
	Message   string `json:"message"`
}

// Upload posts videoPath to all specified platforms with the given title.
func (c *UploadPostClient) Upload(videoPath, title string, platforms []models.Platform) error {
	f, err := os.Open(videoPath)
	if err != nil {
		return fmt.Errorf("open video: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	w.WriteField("user", c.username)
	w.WriteField("title", clamp(title, 2200))
	w.WriteField("privacy_level", c.privacyLevel)

	for i, p := range platforms {
		w.WriteField(fmt.Sprintf("platform[%d]", i), string(p))
	}

	fw, err := w.CreateFormFile("video", filepath.Base(videoPath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return err
	}
	w.Close()

	req, err := http.NewRequest("POST", uploadPostBase+"/api/upload_video", &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Apikey "+c.apiKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()

	var result uploadResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("upload-post error: %s", result.Message)
	}
	return nil
}

func clamp(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
