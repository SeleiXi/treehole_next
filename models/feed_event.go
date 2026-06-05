package models

import "time"

const (
	FeedEventImpression = "impression"
	FeedEventOpen       = "open"
	FeedEventClick      = "click"
	FeedEventReply      = "reply"
	FeedEventFavorite   = "favorite"
	FeedEventSubscribe  = "subscribe"
	FeedEventHide       = "hide"
	FeedEventReport     = "report"
)

type FeedEvent struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    int       `json:"user_id" gorm:"index:idx_feed_event_user_created,priority:1;index:idx_feed_event_user_type_created_hole,priority:1;index:idx_feed_event_request_dedupe,priority:2"`
	HoleID    int       `json:"hole_id" gorm:"index;index:idx_feed_event_user_type_created_hole,priority:4;index:idx_feed_event_type_created_hole,priority:3;index:idx_feed_event_request_dedupe,priority:4"`
	EventType string    `json:"event_type" gorm:"type:varchar(32);not null;index;index:idx_feed_event_user_type_created_hole,priority:2;index:idx_feed_event_type_created_hole,priority:1;index:idx_feed_event_request_dedupe,priority:3"`
	FeedMode  string    `json:"feed_mode" gorm:"type:varchar(32);not null;index"`
	Position  int       `json:"position"`
	RequestID string    `json:"request_id" gorm:"type:varchar(64);index;index:idx_feed_event_request_dedupe,priority:1"`
	CreatedAt time.Time `json:"created_at" gorm:"index:idx_feed_event_user_created,priority:2;index:idx_feed_event_user_type_created_hole,priority:3;index:idx_feed_event_type_created_hole,priority:2"`
}
