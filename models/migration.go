package models

import "gorm.io/gorm"

func MigrateRecsysTables(db *gorm.DB) error {
	if db.Dialector.Name() == "mysql" {
		return migrateRecsysTablesMySQL(db)
	}
	return db.AutoMigrate(&HoleFeature{}, &FeedEvent{}, &SearchEvent{})
}

func migrateRecsysTablesMySQL(db *gorm.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS hole_feature (
			hole_id bigint NOT NULL,
			reply1h bigint DEFAULT NULL,
			reply6h bigint DEFAULT NULL,
			reply24h bigint DEFAULT NULL,
			view1h bigint DEFAULT NULL,
			view24h bigint DEFAULT NULL,
			favorite_count bigint DEFAULT NULL,
			subscription_count bigint DEFAULT NULL,
			hot_score double DEFAULT NULL,
			quality_score double DEFAULT NULL,
			controversy_score double DEFAULT NULL,
			updated_at datetime(3) DEFAULT NULL,
			PRIMARY KEY (hole_id),
			KEY idx_hole_feature_hot_score (hot_score),
			KEY idx_hole_feature_quality_score (quality_score),
			KEY idx_hole_feature_updated_at (updated_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS feed_event (
			id bigint unsigned NOT NULL AUTO_INCREMENT,
			user_id bigint DEFAULT NULL,
			hole_id bigint DEFAULT NULL,
			event_type varchar(32) NOT NULL,
			feed_mode varchar(32) NOT NULL,
			position bigint DEFAULT NULL,
			request_id varchar(64) DEFAULT NULL,
			request_dedup_key varchar(191) DEFAULT NULL,
			created_at datetime(3) DEFAULT NULL,
			PRIMARY KEY (id),
			UNIQUE KEY idx_feed_event_request_dedup_key (request_dedup_key),
			KEY idx_feed_event_user_created (user_id, created_at),
			KEY idx_feed_event_user_type_created_hole (user_id, event_type, created_at, hole_id),
			KEY idx_feed_event_request_dedupe (request_id, user_id, event_type, hole_id),
			KEY idx_feed_event_hole_id (hole_id),
			KEY idx_feed_event_type_created_hole (event_type, created_at, hole_id),
			KEY idx_feed_event_feed_mode (feed_mode),
			KEY idx_feed_event_request_id (request_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS search_event (
			id bigint unsigned NOT NULL AUTO_INCREMENT,
			user_id bigint DEFAULT NULL,
			query_hash char(64) NOT NULL,
			query_length bigint DEFAULT NULL,
			query_term_count bigint DEFAULT NULL,
			accurate tinyint(1) DEFAULT NULL,
			source varchar(32) NOT NULL,
			floor_id bigint DEFAULT NULL,
			hole_id bigint DEFAULT NULL,
			event_type varchar(32) NOT NULL,
			position bigint DEFAULT NULL,
			base_rank bigint DEFAULT NULL,
			base_score double DEFAULT NULL,
			request_id varchar(64) DEFAULT NULL,
			request_dedup_key varchar(191) DEFAULT NULL,
			created_at datetime(3) DEFAULT NULL,
			PRIMARY KEY (id),
			UNIQUE KEY idx_search_event_request_dedup_key (request_dedup_key),
			KEY idx_search_event_user_created (user_id, created_at),
			KEY idx_search_event_user_query_created (user_id, query_hash, created_at),
			KEY idx_search_event_user_hole_created (user_id, hole_id, created_at),
			KEY idx_search_event_request_dedupe (request_id, user_id, event_type, floor_id),
			KEY idx_search_event_query_created (query_hash, created_at),
			KEY idx_search_event_source (source),
			KEY idx_search_event_floor_id (floor_id),
			KEY idx_search_event_hole_id (hole_id),
			KEY idx_search_event_event_type (event_type),
			KEY idx_search_event_request_id (request_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}
