package db

import (
	"database/sql"
	"encoding/json"
	"nichebot/models"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
}

func New(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	d := &DB{conn: conn}
	return d, d.migrate()
}

func (d *DB) migrate() error {
	_, err := d.conn.Exec(`
		CREATE TABLE IF NOT EXISTS channels (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			niche TEXT NOT NULL,
			platforms TEXT NOT NULL DEFAULT '[]',
			videos_per_day INTEGER NOT NULL DEFAULT 3,
			status TEXT NOT NULL DEFAULT 'active',
			created_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS posts (
			id TEXT PRIMARY KEY,
			channel_id TEXT NOT NULL,
			channel_name TEXT NOT NULL,
			topic TEXT NOT NULL,
			video_path TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			platforms_posted TEXT NOT NULL DEFAULT '[]',
			error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			completed_at TEXT
		);

		CREATE INDEX IF NOT EXISTS idx_posts_channel_id ON posts(channel_id);
		CREATE INDEX IF NOT EXISTS idx_posts_status ON posts(status);
		CREATE INDEX IF NOT EXISTS idx_posts_created_at ON posts(created_at);
	`)
	if err != nil {
		return err
	}
	// Additive migrations — safe to run on existing DBs (duplicate-column errors ignored).
	d.conn.Exec(`ALTER TABLE channels ADD COLUMN cta_text TEXT NOT NULL DEFAULT ''`)
	d.conn.Exec(`ALTER TABLE channels ADD COLUMN affiliate_link TEXT NOT NULL DEFAULT ''`)
	d.conn.Exec(`ALTER TABLE channels ADD COLUMN video_language TEXT NOT NULL DEFAULT ''`)
	return nil
}

func (d *DB) SaveChannel(ch models.Channel) error {
	platforms, _ := json.Marshal(ch.Platforms)
	_, err := d.conn.Exec(`
		INSERT OR REPLACE INTO channels
			(id, name, niche, platforms, videos_per_day, status, created_at, cta_text, affiliate_link, video_language)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, ch.ID, ch.Name, ch.Niche, string(platforms), ch.VideosPerDay,
		string(ch.Status), ch.CreatedAt.Format(time.RFC3339), ch.CTAText, ch.AffiliateLink, ch.VideoLanguage)
	return err
}

func (d *DB) GetChannels() ([]models.Channel, error) {
	rows, err := d.conn.Query(`
		SELECT id, name, niche, platforms, videos_per_day, status, created_at, cta_text, affiliate_link, video_language
		FROM channels ORDER BY created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []models.Channel
	for rows.Next() {
		var ch models.Channel
		var platformsJSON, statusStr, createdAt string
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Niche, &platformsJSON,
			&ch.VideosPerDay, &statusStr, &createdAt, &ch.CTAText, &ch.AffiliateLink, &ch.VideoLanguage); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(platformsJSON), &ch.Platforms)
		ch.Status = models.ChannelStatus(statusStr)
		ch.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		channels = append(channels, ch)
	}
	return channels, nil
}

func (d *DB) GetPost(id string) (models.Post, error) {
	rows, err := d.conn.Query(`
		SELECT id, channel_id, channel_name, topic, video_path, status,
		       platforms_posted, error, created_at, completed_at
		FROM posts WHERE id = ? LIMIT 1
	`, id)
	if err != nil {
		return models.Post{}, err
	}
	defer rows.Close()
	posts, err := scanPosts(rows)
	if err != nil || len(posts) == 0 {
		return models.Post{}, err
	}
	return posts[0], nil
}

func (d *DB) GetPendingPosts(channelID string) ([]models.Post, error) {
	rows, err := d.conn.Query(`
		SELECT id, channel_id, channel_name, topic, video_path, status,
		       platforms_posted, error, created_at, completed_at
		FROM posts WHERE channel_id = ? AND status = 'pending_approval'
		ORDER BY created_at DESC
	`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPosts(rows)
}

func (d *DB) GetChannelPosts(channelID string, limit int) ([]models.Post, error) {
	rows, err := d.conn.Query(`
		SELECT id, channel_id, channel_name, topic, video_path, status,
		       platforms_posted, error, created_at, completed_at
		FROM posts WHERE channel_id = ? AND status NOT IN ('pending_approval', 'generating')
		ORDER BY created_at DESC LIMIT ?
	`, channelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPosts(rows)
}

func (d *DB) UpdatePostStatus(id string, status models.PostStatus) error {
	_, err := d.conn.Exec(`UPDATE posts SET status = ? WHERE id = ?`, string(status), id)
	return err
}

func (d *DB) UpdateChannelStatus(id string, status models.ChannelStatus) error {
	_, err := d.conn.Exec(`UPDATE channels SET status = ? WHERE id = ?`, string(status), id)
	return err
}

func (d *DB) DeleteChannel(id string) error {
	_, err := d.conn.Exec(`DELETE FROM channels WHERE id = ?`, id)
	return err
}

func (d *DB) SavePost(p models.Post) error {
	platforms, _ := json.Marshal(p.PlatformsPosted)
	var completedAt interface{}
	if p.CompletedAt != nil {
		completedAt = p.CompletedAt.Format(time.RFC3339)
	}
	_, err := d.conn.Exec(`
		INSERT OR REPLACE INTO posts
			(id, channel_id, channel_name, topic, video_path, status, platforms_posted, error, created_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, p.ID, p.ChannelID, p.ChannelName, p.Topic, p.VideoPath,
		string(p.Status), string(platforms), p.Error,
		p.CreatedAt.Format(time.RFC3339), completedAt)
	return err
}

func (d *DB) GetRecentPosts(limit int) ([]models.Post, error) {
	rows, err := d.conn.Query(`
		SELECT id, channel_id, channel_name, topic, video_path, status,
		       platforms_posted, error, created_at, completed_at
		FROM posts ORDER BY created_at DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPosts(rows)
}

func (d *DB) GetTodayPostCount(channelID string) (int, error) {
	today := time.Now().Format("2006-01-02")
	var count int
	err := d.conn.QueryRow(`
		SELECT COUNT(*) FROM posts
		WHERE channel_id = ? AND status = 'done' AND created_at LIKE ?
	`, channelID, today+"%").Scan(&count)
	return count, err
}

func (d *DB) GetTotalPostCount(channelID string) (int, error) {
	var count int
	err := d.conn.QueryRow(`
		SELECT COUNT(*) FROM posts WHERE channel_id = ? AND status = 'done'
	`, channelID).Scan(&count)
	return count, err
}

// GetTopTopics returns the most-used topics for a channel (repeat what works)
func (d *DB) GetTopTopics(channelID string, limit int) ([]string, error) {
	rows, err := d.conn.Query(`
		SELECT topic, COUNT(*) as cnt
		FROM posts
		WHERE channel_id = ? AND status = 'done'
		GROUP BY topic ORDER BY cnt DESC LIMIT ?
	`, channelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topics []string
	for rows.Next() {
		var topic string
		var cnt int
		rows.Scan(&topic, &cnt)
		topics = append(topics, topic)
	}
	return topics, nil
}

// GetStats returns summary stats for dashboard
type ChannelStats struct {
	TodayCount  int
	TotalCount  int
	FailCount   int
	ActiveCount int // generating + pending_approval + posting
}

func (d *DB) GetChannelStats(channelID string) (ChannelStats, error) {
	today := time.Now().Format("2006-01-02")
	var s ChannelStats

	d.conn.QueryRow(`SELECT COUNT(*) FROM posts WHERE channel_id = ? AND status = 'done' AND created_at LIKE ?`,
		channelID, today+"%").Scan(&s.TodayCount)
	d.conn.QueryRow(`SELECT COUNT(*) FROM posts WHERE channel_id = ? AND status = 'done'`,
		channelID).Scan(&s.TotalCount)
	d.conn.QueryRow(`SELECT COUNT(*) FROM posts WHERE channel_id = ? AND status = 'failed'`,
		channelID).Scan(&s.FailCount)
	d.conn.QueryRow(`SELECT COUNT(*) FROM posts WHERE channel_id = ? AND status IN ('generating','pending_approval','posting')`,
		channelID).Scan(&s.ActiveCount)
	return s, nil
}

// GetActivePostCount counts posts currently in the pipeline (generating/pending_approval/posting).
func (d *DB) GetActivePostCount(channelID string) (int, error) {
	var count int
	err := d.conn.QueryRow(`
		SELECT COUNT(*) FROM posts
		WHERE channel_id = ? AND status IN ('generating','pending_approval','posting')
	`, channelID).Scan(&count)
	return count, err
}

// ResetOrphanedPosts marks any posts stuck in generating/posting as failed.
// Called on startup to clean up from a previous crash or restart.
func (d *DB) ResetOrphanedPosts() error {
	_, err := d.conn.Exec(`
		UPDATE posts SET status = 'failed', error = 'interrupted: process restarted'
		WHERE status IN ('generating','posting')
	`)
	return err
}

func scanPosts(rows *sql.Rows) ([]models.Post, error) {
	var posts []models.Post
	for rows.Next() {
		var p models.Post
		var platformsJSON, statusStr, createdAt string
		var completedAt *string
		if err := rows.Scan(&p.ID, &p.ChannelID, &p.ChannelName, &p.Topic, &p.VideoPath,
			&statusStr, &platformsJSON, &p.Error, &createdAt, &completedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(platformsJSON), &p.PlatformsPosted)
		p.Status = models.PostStatus(statusStr)
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		if completedAt != nil {
			t, _ := time.Parse(time.RFC3339, *completedAt)
			p.CompletedAt = &t
		}
		posts = append(posts, p)
	}
	return posts, nil
}
