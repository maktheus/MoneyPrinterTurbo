package models

import (
	"encoding/json"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

type Platform string

const (
	PlatformTikTok    Platform = "tiktok"
	PlatformInstagram Platform = "instagram"
	PlatformFacebook  Platform = "facebook"
	PlatformYouTube   Platform = "youtube"
)

type ChannelStatus string

const (
	StatusActive ChannelStatus = "active"
	StatusPaused ChannelStatus = "paused"
)

type PostStatus string

const (
	PostGenerating PostStatus = "generating"
	PostPosting    PostStatus = "posting"
	PostDone       PostStatus = "done"
	PostFailed     PostStatus = "failed"
)

type Channel struct {
	ID           string
	Name         string
	Niche        string
	Platforms    []Platform
	VideosPerDay int
	Status       ChannelStatus
	CreatedAt    time.Time
}

func (c Channel) PlatformsJSON() string {
	b, _ := json.Marshal(c.Platforms)
	return string(b)
}

type Post struct {
	ID              string
	ChannelID       string
	ChannelName     string
	Topic           string
	VideoPath       string
	Status          PostStatus
	PlatformsPosted []Platform
	Error           string
	CreatedAt       time.Time
	CompletedAt     *time.Time
}

type Config struct {
	MPT        MPTConfig        `toml:"mpt"`
	UploadPost UploadPostConfig `toml:"upload_post"`
}

type MPTConfig struct {
	BaseURL       string `toml:"base_url"`
	VideoAspect   string `toml:"video_aspect"`
	VoiceName     string `toml:"voice_name"`
	VideoLanguage string `toml:"video_language"`
}

type UploadPostConfig struct {
	APIKey       string `toml:"api_key"`
	Username     string `toml:"username"`
	PrivacyLevel string `toml:"privacy_level"`
}

func LoadConfig(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func DefaultConfig() *Config {
	return &Config{
		MPT: MPTConfig{
			BaseURL:       "http://localhost:8080",
			VideoAspect:   "9:16",
			VoiceName:     "en-US-JennyNeural-Female",
			VideoLanguage: "en",
		},
		UploadPost: UploadPostConfig{
			APIKey:       "your-upload-post-api-key",
			Username:     "your-upload-post-username",
			PrivacyLevel: "PUBLIC_TO_EVERYONE",
		},
	}
}

func (c *Config) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}
