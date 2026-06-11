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
	return err
}

func (d *DB) SaveChannel(ch models.Channel) error {
	platforms, _ := json.Marshal(ch.Platforms)
	_, err := d.conn.Exec(`
		INSERT OR REPLACE INTO channels (id, name, niche, platforms, videos_per_day, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, ch.ID, ch.Name, ch.Niche, string(platforms), ch.VideosPerDay,
		string(ch.Status), ch.CreatedAt.Format(time.RFC3339))
	return err
}

func (d *DB) GetChannels() ([]models.Channel, error) {
	rows, err := d.conn.Query(`
		SELECT id, name, niche, platforms, videos_per_day, status, created_at
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
			&ch.VideosPerDay, &statusStr, &createdAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(platformsJSON), &ch.Platforms)
		ch.Status = models.ChannelStatus(statusStr)
		ch.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		channels = append(channels, ch)
	}
	return channels, nil
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
	TodayCount int
	TotalCount int
	FailCount  int
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
	return s, nil
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
