package models

import "time"

type FeedEvent struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    int       `json:"user_id" gorm:"index:idx_feed_event_user_created,priority:1"`
	HoleID    int       `json:"hole_id" gorm:"index"`
	EventType string    `json:"event_type" gorm:"type:varchar(32);not null;index"`
	FeedMode  string    `json:"feed_mode" gorm:"type:varchar(32);not null;index"`
	Position  int       `json:"position"`
	RequestID string    `json:"request_id" gorm:"type:varchar(64);index"`
	CreatedAt time.Time `json:"created_at" gorm:"index:idx_feed_event_user_created,priority:2"`
}
