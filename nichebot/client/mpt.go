package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"nichebot/models"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type MPTClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewMPTClient(baseURL string) *MPTClient {
	return &MPTClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type videoRequest struct {
	VideoSubject  string `json:"video_subject"`
	VideoAspect   string `json:"video_aspect"`
	VoiceName     string `json:"voice_name"`
	VideoLanguage string `json:"video_language"`
	VideoCount    int    `json:"video_count"`
	BgmType       string `json:"bgm_type"`
}

type createTaskResp struct {
	Status int `json:"status"`
	Data   struct {
		TaskID string `json:"task_id"`
	} `json:"data"`
}

type taskStatusResp struct {
	Status int `json:"status"`
	Data   struct {
		State    int      `json:"state"`
		Progress int      `json:"progress"`
		Videos   []string `json:"videos"`
	} `json:"data"`
}

// MPT task state constants (from app/models/const.py)
const (
	stateComplete   = 1
	stateFailed     = -1
	stateProcessing = 4
)

const apiPrefix = "/api/v1"

// GenerateVideo submits a video generation task and returns the task ID.
func (c *MPTClient) GenerateVideo(cfg *models.MPTConfig, subject string) (string, error) {
	req := videoRequest{
		VideoSubject:  subject,
		VideoAspect:   cfg.VideoAspect,
		VoiceName:     cfg.VoiceName,
		VideoLanguage: cfg.VideoLanguage,
		VideoCount:    1,
		BgmType:       "random",
	}

	body, _ := json.Marshal(req)
	resp, err := c.httpClient.Post(c.baseURL+apiPrefix+"/videos", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	var result createTaskResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if result.Status != 200 {
		return "", fmt.Errorf("API returned status %d", result.Status)
	}
	return result.Data.TaskID, nil
}

// WaitForTask polls until the task finishes and returns the local path to the downloaded video.
// onProgress is called with 0-100 whenever progress updates.
func (c *MPTClient) WaitForTask(taskID string, onProgress func(int)) (string, error) {
	poll := &http.Client{Timeout: 15 * time.Second}

	for {
		time.Sleep(10 * time.Second)

		resp, err := poll.Get(fmt.Sprintf("%s%s/tasks/%s", c.baseURL, apiPrefix, taskID))
		if err != nil {
			continue
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result taskStatusResp
		if err := json.Unmarshal(raw, &result); err != nil {
			continue
		}

		if onProgress != nil {
			onProgress(result.Data.Progress)
		}

		switch result.Data.State {
		case stateComplete:
			if len(result.Data.Videos) == 0 {
				return "", fmt.Errorf("task complete but no videos in response")
			}
			return c.downloadVideo(taskID, result.Data.Videos[0])
		case stateFailed:
			return "", fmt.Errorf("video generation task failed")
		}
	}
}

func (c *MPTClient) downloadVideo(taskID, videoURL string) (string, error) {
	dir := filepath.Join("downloads", taskID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	outPath := filepath.Join(dir, "video.mp4")

	// MPT may return a relative path ("/tasks/...") when no endpoint is configured.
	if !strings.HasPrefix(videoURL, "http") {
		videoURL = c.baseURL + videoURL
	}

	resp, err := c.httpClient.Get(videoURL)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	f, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", err
	}
	return outPath, nil
}
