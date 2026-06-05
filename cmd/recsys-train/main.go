package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"treehole_next/recsys/modelrank"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type sample struct {
	label    float64
	features map[string]float64
}

func main() {
	var (
		dbURL  = flag.String("db", os.Getenv("DB_URL"), "MySQL DSN, for example user:pass@tcp(host:3306)/treehole?parseTime=true&loc=Asia%2fShanghai")
		task   = flag.String("task", "search", "training task: search or home")
		out    = flag.String("out", "recsys_model.json", "output model JSON path")
		days   = flag.Int("days", 30, "lookback days")
		limit  = flag.Int("limit", 200000, "maximum training rows")
		epochs = flag.Int("epochs", 8, "SGD epochs")
		lr     = flag.Float64("lr", 0.03, "SGD learning rate")
		l2     = flag.Float64("l2", 0.0001, "L2 regularization")
	)
	flag.Parse()

	if *dbURL == "" {
		fatal(errors.New("missing -db or DB_URL"))
	}
	db, err := gorm.Open(mysql.Open(*dbURL), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	if err != nil {
		fatal(err)
	}

	var samples []sample
	switch *task {
	case "search":
		samples, err = loadSearchSamples(db, *days, *limit)
	case "home", "feed", "recommend":
		samples, err = loadHomeSamples(db, *days, *limit)
	default:
		err = fmt.Errorf("unknown task %q", *task)
	}
	if err != nil {
		fatal(err)
	}
	if len(samples) == 0 {
		fatal(errors.New("no training samples found"))
	}

	model := train(samples, *epochs, *lr, *l2)
	model.Task = *task
	model.Version = fmt.Sprintf("%s-%s", *task, time.Now().UTC().Format("20060102T150405Z"))
	minScore := -50.0
	maxScore := 50.0
	model.MinScore = &minScore
	model.MaxScore = &maxScore

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil && filepath.Dir(*out) != "." {
		fatal(err)
	}
	data, err := json.MarshalIndent(model, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		fatal(err)
	}
	pos, neg := labelStats(samples)
	fmt.Fprintf(os.Stderr, "trained %s model: samples=%d positives=%d negatives=%d features=%d out=%s\n", *task, len(samples), pos, neg, len(model.Weights), *out)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

type searchRow struct {
	Label             float64
	QueryLength       int
	QueryTermCount    int
	Position          int
	BaseRank          int
	BaseScore         *float64
	Accurate          bool
	Source            string
	FloorCreatedAt    time.Time
	FloorLike         int
	FloorDislike      int
	FloorRanking      int
	ContentLength     int
	HoleCreatedAt     time.Time
	HoleUpdatedAt     time.Time
	HoleReply         int
	HoleView          int
	HoleGood          bool
	HoleLocked        bool
	HoleFrozen        bool
	DivisionID        int
	FavoriteCount     int
	SubscriptionCount int
	TagCount          int
	FeatureUpdatedAt  *time.Time
	HotScore          *float64
	QualityScore      *float64
	ControversyScore  *float64
	Reply24h          *int
	View24h           *int
	SampleAt          time.Time
}

func loadSearchSamples(db *gorm.DB, days int, limit int) ([]sample, error) {
	var rows []searchRow
	err := db.Raw(`
		SELECT
			CASE WHEN EXISTS (
				SELECT 1 FROM feed_event fe
				WHERE fe.user_id = se.user_id
					AND fe.hole_id = se.hole_id
					AND fe.event_type IN ('open', 'click', 'reply', 'favorite', 'subscribe')
					AND fe.created_at >= se.created_at
					AND fe.created_at < DATE_ADD(se.created_at, INTERVAL 30 MINUTE)
			) THEN 1 ELSE 0 END AS label,
			se.query_length,
			se.query_term_count,
			se.position,
			se.base_rank,
			se.base_score,
			se.accurate,
			se.source,
			f.created_at AS floor_created_at,
			f.`+"`like`"+` AS floor_like,
			f.dislike AS floor_dislike,
			f.ranking AS floor_ranking,
			CHAR_LENGTH(f.content) AS content_length,
			h.created_at AS hole_created_at,
			h.updated_at AS hole_updated_at,
			h.reply AS hole_reply,
			h.view AS hole_view,
			h.good AS hole_good,
			h.locked AS hole_locked,
			h.frozen AS hole_frozen,
			h.division_id,
			h.favorite_count,
			h.subscription_count,
			COALESCE(tc.tag_count, 0) AS tag_count,
			hf.updated_at AS feature_updated_at,
			hf.hot_score,
			hf.quality_score,
			hf.controversy_score,
			hf.reply24h,
			hf.view24h,
			se.created_at AS sample_at
		FROM search_event se
		JOIN floor f ON f.id = se.floor_id
		JOIN hole h ON h.id = se.hole_id
		LEFT JOIN hole_feature hf ON hf.hole_id = se.hole_id
		LEFT JOIN (
			SELECT hole_id, COUNT(*) AS tag_count FROM hole_tags GROUP BY hole_id
		) tc ON tc.hole_id = se.hole_id
		WHERE se.event_type = 'impression'
			AND se.created_at >= DATE_SUB(NOW(), INTERVAL ? DAY)
		ORDER BY se.created_at DESC
		LIMIT ?
	`, days, limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	samples := make([]sample, 0, len(rows))
	for _, row := range rows {
		samples = append(samples, sample{label: row.Label, features: searchFeatures(row)})
	}
	return samples, nil
}

func searchFeatures(row searchRow) map[string]float64 {
	now := row.SampleAt
	if now.IsZero() {
		now = time.Now()
	}
	holeAgeHours := elapsedHours(now, row.HoleCreatedAt)
	holeUpdateHours := elapsedHours(now, row.HoleUpdatedAt)
	floorAgeHours := elapsedHours(now, row.FloorCreatedAt)
	features := map[string]float64{
		"base_rank":             float64(row.BaseRank),
		"inverse_base_rank":     1 / float64(row.BaseRank+1),
		"query_length":          float64(row.QueryLength),
		"query_term_count":      float64(row.QueryTermCount),
		"content_length_log":    math.Log1p(float64(row.ContentLength)),
		"floor_age_hours_log":   math.Log1p(floorAgeHours),
		"floor_like_log":        math.Log1p(float64(row.FloorLike)),
		"floor_dislike_log":     math.Log1p(float64(row.FloorDislike)),
		"floor_ranking":         float64(row.FloorRanking),
		"hole_reply_log":        math.Log1p(float64(row.HoleReply)),
		"hole_view_log":         math.Log1p(float64(row.HoleView)),
		"hole_favorite_log":     math.Log1p(float64(row.FavoriteCount)),
		"hole_subscription_log": math.Log1p(float64(row.SubscriptionCount)),
		"hole_age_hours_log":    math.Log1p(holeAgeHours),
		"hole_update_hours_log": math.Log1p(holeUpdateHours),
		"hole_freshness_24h":    24.0 / (24.0 + holeUpdateHours),
		"hole_division_id":      float64(row.DivisionID),
		"hole_tag_count":        float64(row.TagCount),
	}
	if row.BaseScore != nil {
		features["base_score"] = *row.BaseScore
		features["base_score_log"] = math.Log1p(math.Max(*row.BaseScore, 0))
	}
	if row.Accurate {
		features["accurate"] = 1
	}
	if row.Source == "elastic" {
		features["source_elastic"] = 1
	}
	if row.FloorRanking == 0 {
		features["is_first_floor"] = 1
	}
	if row.HoleGood {
		features["hole_is_good"] = 1
	}
	if row.HoleLocked {
		features["hole_is_locked"] = 1
	}
	if row.HoleFrozen {
		features["hole_is_frozen"] = 1
	}
	if row.FeatureUpdatedAt != nil && row.FeatureUpdatedAt.After(now.Add(-15*time.Minute)) {
		if row.HotScore != nil {
			features["feature_hot_score"] = *row.HotScore
		}
		if row.QualityScore != nil {
			features["feature_quality_score"] = *row.QualityScore
		}
		if row.ControversyScore != nil {
			features["feature_controversy_score"] = *row.ControversyScore
		}
		if row.Reply24h != nil {
			features["feature_reply_24h_log"] = math.Log1p(float64(*row.Reply24h))
		}
		if row.View24h != nil {
			features["feature_view_24h_log"] = math.Log1p(float64(*row.View24h))
		}
	}
	return features
}

type homeRow struct {
	Label             float64
	Reply             int
	View              int
	Good              bool
	Locked            bool
	Frozen            bool
	DivisionID        int
	FavoriteCount     int
	SubscriptionCount int
	HoleCreatedAt     time.Time
	HoleUpdatedAt     time.Time
	TagCount          int
	OpenCount         int
	ImpressionCount   int
	FeatureUpdatedAt  *time.Time
	HotScore          *float64
	QualityScore      *float64
	ControversyScore  *float64
	Reply1h           *int
	Reply6h           *int
	Reply24h          *int
	View1h            *int
	View24h           *int
	SampleAt          time.Time
}

func loadHomeSamples(db *gorm.DB, days int, limit int) ([]sample, error) {
	var rows []homeRow
	err := db.Raw(`
		SELECT
			MAX(CASE
				WHEN fe.event_type IN ('favorite', 'subscribe') THEN 1
				WHEN fe.event_type = 'reply' THEN 0.9
				WHEN fe.event_type IN ('open', 'click') THEN 0.65
				WHEN fe.event_type IN ('hide', 'report') THEN 0
				ELSE 0
			END) AS label,
			h.reply,
			h.view,
			h.good,
			h.locked,
			h.frozen,
			h.division_id,
			h.favorite_count,
			h.subscription_count,
			h.created_at AS hole_created_at,
			h.updated_at AS hole_updated_at,
			COALESCE(tc.tag_count, 0) AS tag_count,
			SUM(CASE WHEN fe.event_type IN ('open', 'click') THEN 1 ELSE 0 END) AS open_count,
			SUM(CASE WHEN fe.event_type = 'impression' THEN 1 ELSE 0 END) AS impression_count,
			hf.updated_at AS feature_updated_at,
			hf.hot_score,
			hf.quality_score,
			hf.controversy_score,
			hf.reply1h,
			hf.reply6h,
			hf.reply24h,
			hf.view1h,
			hf.view24h,
			MAX(fe.created_at) AS sample_at
		FROM feed_event fe
		JOIN hole h ON h.id = fe.hole_id
		LEFT JOIN hole_feature hf ON hf.hole_id = fe.hole_id
		LEFT JOIN (
			SELECT hole_id, COUNT(*) AS tag_count FROM hole_tags GROUP BY hole_id
		) tc ON tc.hole_id = fe.hole_id
		WHERE fe.created_at >= DATE_SUB(NOW(), INTERVAL ? DAY)
		GROUP BY fe.user_id, fe.hole_id, h.id, hf.hole_id, tc.tag_count
		ORDER BY sample_at DESC
		LIMIT ?
	`, days, limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	samples := make([]sample, 0, len(rows))
	for _, row := range rows {
		samples = append(samples, sample{label: row.Label, features: homeFeatures(row)})
	}
	return samples, nil
}

func homeFeatures(row homeRow) map[string]float64 {
	now := row.SampleAt
	if now.IsZero() {
		now = time.Now()
	}
	ageHours := elapsedHours(now, row.HoleCreatedAt)
	updateHours := elapsedHours(now, row.HoleUpdatedAt)
	reply24h := float64(row.Reply)
	view24h := float64(row.View)
	if row.FeatureUpdatedAt != nil && row.FeatureUpdatedAt.After(now.Add(-15*time.Minute)) {
		if row.Reply24h != nil {
			reply24h = float64(*row.Reply24h)
		}
		if row.View24h != nil {
			view24h = float64(*row.View24h)
		}
	}
	ruleScore := 2.5*math.Log1p(reply24h) +
		2.0*math.Log1p(float64(row.FavoriteCount)) +
		1.5*math.Log1p(float64(row.SubscriptionCount)) +
		0.4*math.Log1p(view24h) +
		3.0*(24.0/(24.0+updateHours))
	if ageHours <= 48 {
		ruleScore += 1.2
	}
	if row.Good {
		ruleScore += 2.0
	}
	if row.Locked || row.Frozen {
		ruleScore -= 1.0
	}

	features := map[string]float64{
		"rule_score":                ruleScore,
		"reply_log":                 math.Log1p(float64(row.Reply)),
		"view_log":                  math.Log1p(float64(row.View)),
		"reply_24h_log":             math.Log1p(reply24h),
		"view_24h_log":              math.Log1p(view24h),
		"favorite_count_log":        math.Log1p(float64(row.FavoriteCount)),
		"subscription_count_log":    math.Log1p(float64(row.SubscriptionCount)),
		"age_hours_log":             math.Log1p(ageHours),
		"update_hours_log":          math.Log1p(updateHours),
		"freshness_24h":             24.0 / (24.0 + updateHours),
		"feedback_open_count":       float64(row.OpenCount),
		"feedback_impression_count": float64(row.ImpressionCount),
		"division_affinity":         0,
		"tag_affinity":              0,
		"tag_count":                 float64(row.TagCount),
	}
	if row.Good {
		features["is_good"] = 1
	}
	if row.Locked {
		features["is_locked"] = 1
	}
	if row.Frozen {
		features["is_frozen"] = 1
	}
	if ageHours <= 48 {
		features["is_fresh_post"] = 1
	}
	if row.FeatureUpdatedAt != nil && row.FeatureUpdatedAt.After(now.Add(-15*time.Minute)) {
		if row.HotScore != nil {
			features["feature_hot_score"] = *row.HotScore
		}
		if row.QualityScore != nil {
			features["feature_quality_score"] = *row.QualityScore
		}
		if row.ControversyScore != nil {
			features["feature_controversy_score"] = *row.ControversyScore
		}
		if row.Reply1h != nil {
			features["feature_reply_1h_log"] = math.Log1p(float64(*row.Reply1h))
		}
		if row.Reply6h != nil {
			features["feature_reply_6h_log"] = math.Log1p(float64(*row.Reply6h))
		}
		if row.View1h != nil {
			features["feature_view_1h_log"] = math.Log1p(float64(*row.View1h))
		}
	}
	return features
}

func train(samples []sample, epochs int, lr float64, l2 float64) modelrank.LinearModel {
	if epochs <= 0 {
		epochs = 1
	}
	featureNames := collectFeatureNames(samples)
	weights := make(map[string]float64, len(featureNames))
	pos, neg := labelStats(samples)
	posWeight := 1.0
	if pos > 0 && neg > pos {
		posWeight = math.Min(float64(neg)/float64(pos), 20)
	}

	intercept := 0.0
	for epoch := 0; epoch < epochs; epoch++ {
		for _, sample := range samples {
			score := intercept
			for _, name := range featureNames {
				score += weights[name] * sample.features[name]
			}
			pred := sigmoid(score)
			grad := pred - sample.label
			if sample.label > 0 {
				grad *= posWeight
			}
			intercept -= lr * grad
			for _, name := range featureNames {
				value := sample.features[name]
				if value == 0 || math.IsNaN(value) || math.IsInf(value, 0) {
					continue
				}
				weights[name] -= lr * (grad*value + l2*weights[name])
			}
		}
	}
	return modelrank.LinearModel{Intercept: intercept, Weights: weights}
}

func collectFeatureNames(samples []sample) []string {
	seen := map[string]bool{}
	for _, sample := range samples {
		for name := range sample.features {
			seen[name] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func labelStats(samples []sample) (int, int) {
	pos := 0
	neg := 0
	for _, sample := range samples {
		if sample.label > 0 {
			pos++
		} else {
			neg++
		}
	}
	return pos, neg
}

func elapsedHours(now time.Time, then time.Time) float64 {
	if now.IsZero() || then.IsZero() || then.After(now) {
		return 0
	}
	return now.Sub(then).Hours()
}

func sigmoid(value float64) float64 {
	if value >= 40 {
		return 1
	}
	if value <= -40 {
		return 0
	}
	return 1 / (1 + math.Exp(-value))
}
