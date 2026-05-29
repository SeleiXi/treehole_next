package models

import "time"

type HoleFeature struct {
	HoleID int `json:"hole_id" gorm:"primaryKey"`

	Reply1h  int `json:"reply_1h"`
	Reply6h  int `json:"reply_6h"`
	Reply24h int `json:"reply_24h"`

	View1h  int `json:"view_1h"`
	View24h int `json:"view_24h"`

	FavoriteCount     int `json:"favorite_count"`
	SubscriptionCount int `json:"subscription_count"`

	HotScore         float64 `json:"hot_score" gorm:"index:idx_hole_feature_hot_score,sort:desc"`
	QualityScore     float64 `json:"quality_score" gorm:"index:idx_hole_feature_quality_score,sort:desc"`
	ControversyScore float64 `json:"controversy_score"`

	UpdatedAt time.Time `json:"updated_at" gorm:"index"`
}
