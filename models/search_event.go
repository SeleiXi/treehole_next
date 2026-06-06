package models

import "time"

const (
	SearchEventImpression = "impression"
	SearchEventOpen       = "open"
	SearchEventClick      = "click"
	SearchEventReply      = "reply"
	SearchEventFavorite   = "favorite"
	SearchEventSubscribe  = "subscribe"
	SearchEventHide       = "hide"
	SearchEventReport     = "report"
)

type SearchEvent struct {
	ID uint `json:"id" gorm:"primaryKey"`

	UserID int `json:"user_id" gorm:"index:idx_search_event_user_created,priority:1;index:idx_search_event_user_query_created,priority:1;index:idx_search_event_user_hole_created,priority:1;index:idx_search_event_req_user_query_floor_type_created,priority:2;index:idx_search_event_request_dedupe,priority:2"`

	QueryHash      string `json:"query_hash" gorm:"type:char(64);not null;index:idx_search_event_query_created,priority:1;index:idx_search_event_user_query_created,priority:2;index:idx_search_event_req_user_query_floor_type_created,priority:3"`
	QueryLength    int    `json:"query_length"`
	QueryTermCount int    `json:"query_term_count"`
	Accurate       bool   `json:"accurate"`
	Source         string `json:"source" gorm:"type:varchar(32);not null;index"`

	FloorID int `json:"floor_id" gorm:"index;index:idx_search_event_req_user_query_floor_type_created,priority:4;index:idx_search_event_request_dedupe,priority:4"`
	HoleID  int `json:"hole_id" gorm:"index;index:idx_search_event_user_hole_created,priority:2"`

	EventType       string   `json:"event_type" gorm:"type:varchar(32);not null;index;index:idx_search_event_type_created,priority:1;index:idx_search_event_req_user_query_floor_type_created,priority:5;index:idx_search_event_request_dedupe,priority:3"`
	Position        int      `json:"position"`
	BaseRank        int      `json:"base_rank"`
	BaseScore       *float64 `json:"base_score"`
	RequestID       string   `json:"request_id" gorm:"type:varchar(64);index;index:idx_search_event_req_user_query_floor_type_created,priority:1;index:idx_search_event_request_dedupe,priority:1"`
	RequestDedupKey *string  `json:"-" gorm:"type:varchar(191);uniqueIndex:idx_search_event_request_dedup_key"`

	CreatedAt time.Time `json:"created_at" gorm:"index:idx_search_event_user_created,priority:2;index:idx_search_event_query_created,priority:2;index:idx_search_event_user_query_created,priority:3;index:idx_search_event_user_hole_created,priority:3;index:idx_search_event_type_created,priority:2;index:idx_search_event_req_user_query_floor_type_created,priority:6"`
}
