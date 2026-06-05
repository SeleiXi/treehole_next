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
	at       time.Time
	group    string
}

func main() {
	var (
		dbURL       = flag.String("db", os.Getenv("DB_URL"), "MySQL DSN, for example user:pass@tcp(host:3306)/treehole?parseTime=true&loc=Asia%2fShanghai")
		task        = flag.String("task", "search", "training task: search or home")
		out         = flag.String("out", "recsys_model.json", "output model JSON path")
		days        = flag.Int("days", 30, "lookback days")
		limit       = flag.Int("limit", 200000, "maximum training rows")
		epochs      = flag.Int("epochs", 8, "SGD epochs")
		lr          = flag.Float64("lr", 0.03, "SGD learning rate")
		l2          = flag.Float64("l2", 0.0001, "L2 regularization")
		evalRatio   = flag.Float64("eval-ratio", 0.2, "newest sample ratio reserved for evaluation")
		metricsOut  = flag.String("metrics-out", "", "optional output JSON path for train/eval metrics")
		topFeatures = flag.Int("top-features", 20, "number of largest absolute weights to print")
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

	trainSamples, evalSamples := splitSamplesByTime(samples, *evalRatio)
	if len(trainSamples) == 0 {
		fatal(errors.New("no training samples after time split"))
	}

	model := train(trainSamples, *epochs, *lr, *l2)
	model.Task = *task
	model.Version = fmt.Sprintf("%s-%s", *task, time.Now().UTC().Format("20060102T150405Z"))
	minScore := -50.0
	maxScore := 50.0
	model.MinScore = &minScore
	model.MaxScore = &maxScore
	metrics := map[string]float64{
		"sample_count": float64(len(samples)),
		"train_count":  float64(len(trainSamples)),
		"eval_count":   float64(len(evalSamples)),
	}
	mergeMetrics(metrics, "train", evaluate(trainSamples, model, *task))
	if len(evalSamples) != 0 {
		mergeMetrics(metrics, "eval", evaluate(evalSamples, model, *task))
	}
	model.TrainingMetrics = metrics

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
	if *metricsOut != "" {
		metricsData, err := json.MarshalIndent(metrics, "", "  ")
		if err != nil {
			fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(*metricsOut), 0o755); err != nil && filepath.Dir(*metricsOut) != "." {
			fatal(err)
		}
		if err := os.WriteFile(*metricsOut, append(metricsData, '\n'), 0o644); err != nil {
			fatal(err)
		}
	}
	pos, neg := labelStats(samples)
	fmt.Fprintf(os.Stderr, "trained %s model: samples=%d train=%d eval=%d positives=%d negatives=%d features=%d out=%s\n", *task, len(samples), len(trainSamples), len(evalSamples), pos, neg, len(model.Weights), *out)
	printMetrics(metrics)
	printTopWeights(model, *topFeatures)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

type searchRow struct {
	Label             float64
	UserID            int
	QueryHash         string
	RequestID         string
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
			COALESCE(
				(
					SELECT MAX(CASE
						WHEN sae.event_type IN ('favorite', 'subscribe') THEN 1.0
						WHEN sae.event_type = 'reply' THEN 0.9
						WHEN sae.event_type IN ('open', 'click') THEN 0.7
						WHEN sae.event_type IN ('hide', 'report') THEN 0.0
						ELSE NULL
					END)
					FROM search_event sae
					WHERE sae.user_id = se.user_id
						AND sae.query_hash = se.query_hash
						AND sae.request_id = se.request_id
						AND sae.floor_id = se.floor_id
						AND sae.event_type IN ('open', 'click', 'reply', 'favorite', 'subscribe', 'hide', 'report')
						AND se.request_id <> ''
						AND sae.created_at >= se.created_at
						AND sae.created_at < DATE_ADD(se.created_at, INTERVAL 2 HOUR)
				),
				(
					SELECT MAX(CASE
						WHEN fe.request_id = se.request_id AND se.request_id <> '' AND fe.event_type IN ('favorite', 'subscribe') THEN 1.0
						WHEN fe.request_id = se.request_id AND se.request_id <> '' AND fe.event_type = 'reply' THEN 0.9
						WHEN fe.request_id = se.request_id AND se.request_id <> '' AND fe.event_type IN ('open', 'click') THEN 0.7
						WHEN fe.event_type IN ('favorite', 'subscribe') THEN 0.85
						WHEN fe.event_type = 'reply' THEN 0.75
						WHEN fe.event_type IN ('open', 'click') THEN 0.55
						WHEN fe.event_type IN ('hide', 'report') THEN 0.0
						ELSE NULL
					END)
					FROM feed_event fe
					WHERE fe.user_id = se.user_id
						AND fe.hole_id = se.hole_id
						AND fe.event_type IN ('open', 'click', 'reply', 'favorite', 'subscribe', 'hide', 'report')
						AND fe.created_at >= se.created_at
						AND fe.created_at < DATE_ADD(se.created_at, INTERVAL 30 MINUTE)
				),
				0
			) AS label,
			se.user_id,
			se.query_hash,
			se.request_id,
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
		samples = append(samples, sample{
			label:    row.Label,
			features: searchFeatures(row),
			at:       row.SampleAt,
			group:    searchGroup(row),
		})
	}
	return samples, nil
}

func searchGroup(row searchRow) string {
	if row.RequestID != "" {
		return row.RequestID
	}
	if row.QueryHash != "" {
		return row.QueryHash
	}
	return fmt.Sprintf("user:%d", row.UserID)
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
	UserID            int
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
			fe.user_id,
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
		samples = append(samples, sample{
			label:    row.Label,
			features: homeFeatures(row),
			at:       row.SampleAt,
			group:    fmt.Sprintf("user:%d", row.UserID),
		})
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
	featureStats := computeFeatureStats(samples, featureNames)
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
				score += weights[name] * normalizedFeature(sample, name, featureStats[name])
			}
			pred := sigmoid(score)
			grad := pred - sample.label
			if sample.label > 0 {
				grad *= posWeight
			}
			intercept -= lr * grad
			for _, name := range featureNames {
				value := normalizedFeature(sample, name, featureStats[name])
				if value == 0 || math.IsNaN(value) || math.IsInf(value, 0) {
					continue
				}
				weights[name] -= lr * (grad*value + l2*weights[name])
			}
		}
	}
	return modelrank.LinearModel{Intercept: intercept, Weights: weights, FeatureStats: featureStats}
}

func computeFeatureStats(samples []sample, featureNames []string) map[string]modelrank.FeatureStats {
	stats := make(map[string]modelrank.FeatureStats, len(featureNames))
	if len(samples) == 0 {
		return stats
	}
	for _, name := range featureNames {
		sum := 0.0
		sumSquares := 0.0
		count := 0.0
		for _, sample := range samples {
			value := sample.features[name]
			if math.IsNaN(value) || math.IsInf(value, 0) {
				continue
			}
			sum += value
			sumSquares += value * value
			count++
		}
		if count == 0 {
			continue
		}
		mean := sum / count
		variance := sumSquares/count - mean*mean
		if variance < 0 {
			variance = 0
		}
		std := math.Sqrt(variance)
		if std < 1e-9 {
			std = 1
		}
		stats[name] = modelrank.FeatureStats{Mean: mean, Std: std}
	}
	return stats
}

func normalizedFeature(sample sample, name string, stats modelrank.FeatureStats) float64 {
	value := sample.features[name]
	if stats.Std == 0 || math.IsNaN(stats.Std) || math.IsInf(stats.Std, 0) {
		return value
	}
	return (value - stats.Mean) / stats.Std
}

func splitSamplesByTime(samples []sample, evalRatio float64) ([]sample, []sample) {
	ordered := append([]sample(nil), samples...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].at.Equal(ordered[j].at) {
			return ordered[i].group < ordered[j].group
		}
		return ordered[i].at.Before(ordered[j].at)
	})
	if evalRatio <= 0 || len(ordered) < 2 {
		return ordered, nil
	}
	if evalRatio >= 1 {
		evalRatio = 0.2
	}
	evalCount := int(math.Round(float64(len(ordered)) * evalRatio))
	if evalCount <= 0 {
		evalCount = 1
	}
	if evalCount >= len(ordered) {
		evalCount = len(ordered) - 1
	}
	split := len(ordered) - evalCount
	return ordered[:split], ordered[split:]
}

func evaluate(samples []sample, model modelrank.LinearModel, task string) map[string]float64 {
	metrics := map[string]float64{}
	if len(samples) == 0 {
		return metrics
	}
	pos, neg := labelStats(samples)
	addMetric(metrics, "positive_count", float64(pos))
	addMetric(metrics, "negative_count", float64(neg))
	addMetric(metrics, "positive_rate", float64(pos)/float64(len(samples)))
	addMetric(metrics, "model_logloss", logLoss(samples, func(sample sample) float64 {
		return model.Score(sample.features)
	}))
	if value, ok := auc(samples, func(sample sample) float64 { return model.Score(sample.features) }); ok {
		addMetric(metrics, "model_auc", value)
	}
	if value, ok := auc(samples, func(sample sample) float64 { return baselineScore(task, sample) }); ok {
		addMetric(metrics, "baseline_auc", value)
	}
	if value, ok := ndcgAt(samples, 10, func(sample sample) float64 { return model.Score(sample.features) }); ok {
		addMetric(metrics, "model_ndcg_10", value)
	}
	if value, ok := ndcgAt(samples, 10, func(sample sample) float64 { return baselineScore(task, sample) }); ok {
		addMetric(metrics, "baseline_ndcg_10", value)
	}
	return metrics
}

func baselineScore(task string, sample sample) float64 {
	switch task {
	case "search":
		if value, ok := sample.features["base_rank"]; ok {
			return -value
		}
		return sample.features["inverse_base_rank"]
	default:
		return sample.features["rule_score"]
	}
}

func logLoss(samples []sample, score func(sample) float64) float64 {
	sum := 0.0
	for _, sample := range samples {
		pred := clampProbability(sigmoid(score(sample)))
		label := sample.label
		if label < 0 {
			label = 0
		}
		if label > 1 {
			label = 1
		}
		sum += -(label*math.Log(pred) + (1-label)*math.Log(1-pred))
	}
	return sum / float64(len(samples))
}

func auc(samples []sample, score func(sample) float64) (float64, bool) {
	type scoredLabel struct {
		score float64
		label bool
	}
	items := make([]scoredLabel, 0, len(samples))
	pos := 0
	neg := 0
	for _, sample := range samples {
		label := sample.label > 0
		if label {
			pos++
		} else {
			neg++
		}
		items = append(items, scoredLabel{score: score(sample), label: label})
	}
	if pos == 0 || neg == 0 {
		return 0, false
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].score < items[j].score
	})
	rankSum := 0.0
	i := 0
	for i < len(items) {
		j := i + 1
		for j < len(items) && items[j].score == items[i].score {
			j++
		}
		avgRank := (float64(i+1) + float64(j)) / 2.0
		for k := i; k < j; k++ {
			if items[k].label {
				rankSum += avgRank
			}
		}
		i = j
	}
	return (rankSum - float64(pos*(pos+1))/2.0) / float64(pos*neg), true
}

func ndcgAt(samples []sample, k int, score func(sample) float64) (float64, bool) {
	if k <= 0 {
		k = 10
	}
	groups := map[string][]sample{}
	for _, sample := range samples {
		group := sample.group
		if group == "" {
			group = "all"
		}
		groups[group] = append(groups[group], sample)
	}

	total := 0.0
	count := 0
	for _, groupSamples := range groups {
		if len(groupSamples) < 2 {
			continue
		}
		idealSamples := append([]sample(nil), groupSamples...)
		sort.SliceStable(idealSamples, func(i, j int) bool {
			return idealSamples[i].label > idealSamples[j].label
		})
		ideal := dcgAt(idealSamples, k, func(sample sample) float64 { return sample.label })
		if ideal <= 0 {
			continue
		}
		ranked := append([]sample(nil), groupSamples...)
		sort.SliceStable(ranked, func(i, j int) bool {
			return score(ranked[i]) > score(ranked[j])
		})
		total += dcgAt(ranked, k, func(sample sample) float64 { return sample.label }) / ideal
		count++
	}
	if count == 0 {
		return 0, false
	}
	return total / float64(count), true
}

func dcgAt(samples []sample, k int, relevance func(sample) float64) float64 {
	limit := k
	if len(samples) < limit {
		limit = len(samples)
	}
	total := 0.0
	for i := 0; i < limit; i++ {
		rel := relevance(samples[i])
		if rel <= 0 {
			continue
		}
		total += (math.Pow(2, rel) - 1) / math.Log2(float64(i)+2)
	}
	return total
}

func mergeMetrics(target map[string]float64, prefix string, metrics map[string]float64) {
	for name, value := range metrics {
		addMetric(target, prefix+"_"+name, value)
	}
}

func addMetric(metrics map[string]float64, name string, value float64) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	metrics[name] = value
}

func printMetrics(metrics map[string]float64) {
	names := make([]string, 0, len(metrics))
	for name := range metrics {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(os.Stderr, "metric %s=%.6f\n", name, metrics[name])
	}
}

func printTopWeights(model modelrank.LinearModel, limit int) {
	if limit <= 0 {
		return
	}
	type weightedFeature struct {
		name   string
		weight float64
	}
	features := make([]weightedFeature, 0, len(model.Weights))
	for name, weight := range model.Weights {
		features = append(features, weightedFeature{name: name, weight: weight})
	}
	sort.SliceStable(features, func(i, j int) bool {
		return math.Abs(features[i].weight) > math.Abs(features[j].weight)
	})
	if len(features) < limit {
		limit = len(features)
	}
	for i := 0; i < limit; i++ {
		fmt.Fprintf(os.Stderr, "weight %s=%.6f\n", features[i].name, features[i].weight)
	}
}

func clampProbability(value float64) float64 {
	const epsilon = 1e-15
	if value < epsilon {
		return epsilon
	}
	if value > 1-epsilon {
		return 1 - epsilon
	}
	return value
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
