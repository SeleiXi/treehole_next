package models

import "gorm.io/gorm"

const recsysMySQLCollation = "utf8mb4_unicode_ci"

func MigrateRecsysTables(db *gorm.DB) error {
	if db.Dialector.Name() == "mysql" {
		return migrateRecsysTablesMySQL(db)
	}
	return db.AutoMigrate(&HoleFeature{}, &FeedEvent{}, &SearchEvent{})
}

func migrateRecsysTablesMySQL(db *gorm.DB) error {
	for _, statement := range recsysMySQLCreateStatements() {
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	return normalizeRecsysTableCollationsMySQL(db)
}

func recsysMySQLCreateStatements() []string {
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
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
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
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
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
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	}
	return statements
}

func normalizeRecsysTableCollationsMySQL(db *gorm.DB) error {
	return normalizeTableCollationMySQL(db, "search_event")
}

func normalizeTableCollationMySQL(db *gorm.DB, table string) error {
	var mismatched int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE()
			AND TABLE_NAME = ?
			AND COLLATION_NAME IS NOT NULL
			AND COLLATION_NAME <> ?
	`, table, recsysMySQLCollation).Scan(&mismatched).Error; err != nil {
		return err
	}
	if mismatched == 0 {
		return nil
	}

	switch table {
	case "search_event":
		return db.Exec(`ALTER TABLE search_event CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci`).Error
	default:
		return nil
	}
}
