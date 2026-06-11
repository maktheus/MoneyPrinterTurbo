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
	PostGenerating      PostStatus = "generating"
	PostPendingApproval PostStatus = "pending_approval" // waiting for user to approve before posting
	PostPosting         PostStatus = "posting"
	PostDone            PostStatus = "done"
	PostFailed          PostStatus = "failed"
	PostRejected        PostStatus = "rejected"
)

type Channel struct {
	ID            string
	Name          string
	Niche         string
	Platforms     []Platform
	VideosPerDay  int
	Status        ChannelStatus
	CreatedAt     time.Time
	CTAText       string // call-to-action appended to every caption
	AffiliateLink string // included in caption only on FB and YouTube (support clickable links)
	VideoLanguage string // per-channel language override; empty = use global MPT config
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
	Telegram   TelegramConfig   `toml:"telegram"`
	UILanguage string           `toml:"ui_language"` // "pt" or "en"
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

type TelegramConfig struct {
	BotToken string `toml:"bot_token"`
	ChatID   string `toml:"chat_id"`
	Enabled  bool   `toml:"enabled"`
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
		UILanguage: "pt",
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
		Telegram: TelegramConfig{
			BotToken: "",
			ChatID:   "",
			Enabled:  false,
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
