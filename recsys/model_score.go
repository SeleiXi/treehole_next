package recsys

import (
	"math"
	"time"

	"github.com/rs/zerolog/log"

	"treehole_next/config"
	"treehole_next/models"
	"treehole_next/recsys/modelrank"
)

func scoreCandidate(hole *models.Hole, feature *models.HoleFeature, feedback userFeedback, tagIDs []int, now time.Time) float64 {
	ruleScore := scoreHole(hole, feature, now) + feedback.affinityScore(hole, tagIDs) - feedback.penalty(hole.ID)
	if !config.Config.RecsysModelRanking {
		return ruleScore
	}
	model, err := modelrank.Load(config.Config.RecsysModelPath)
	if err != nil {
		log.Warn().Err(err).Str("path", config.Config.RecsysModelPath).Msg("could not load recsys rank model")
		return ruleScore
	}
	if model == nil {
		return ruleScore
	}
	return model.Score(candidateFeatures(hole, feature, feedback, tagIDs, now, ruleScore))
}

func candidateFeatures(hole *models.Hole, feature *models.HoleFeature, feedback userFeedback, tagIDs []int, now time.Time, ruleScore float64) map[string]float64 {
	reply24h := float64(hole.Reply)
	view24h := float64(hole.View)
	freshFeature := feature != nil && feature.UpdatedAt.After(now.Add(-featureMaxAge))
	if freshFeature {
		reply24h = float64(feature.Reply24h)
		view24h = float64(feature.View24h)
	}

	ageHours := math.Max(now.Sub(hole.CreatedAt).Hours(), 0)
	updateHours := math.Max(now.Sub(hole.UpdatedAt).Hours(), 0)
	divisionAffinity := feedback.divisions[hole.DivisionID]
	tagAffinity := 0.0
	for _, tagID := range tagIDs {
		tagAffinity += feedback.tags[tagID]
	}

	features := map[string]float64{
		"rule_score":                ruleScore,
		"reply_log":                 math.Log1p(float64(hole.Reply)),
		"view_log":                  math.Log1p(float64(hole.View)),
		"reply_24h_log":             math.Log1p(reply24h),
		"view_24h_log":              math.Log1p(view24h),
		"favorite_count_log":        math.Log1p(float64(hole.FavoriteCount)),
		"subscription_count_log":    math.Log1p(float64(hole.SubscriptionCount)),
		"age_hours_log":             math.Log1p(ageHours),
		"update_hours_log":          math.Log1p(updateHours),
		"freshness_24h":             24.0 / (24.0 + updateHours),
		"feedback_open_count":       float64(feedback.opened[hole.ID]),
		"feedback_impression_count": float64(feedback.impressions[hole.ID]),
		"division_affinity":         divisionAffinity,
		"tag_affinity":              tagAffinity,
		"tag_count":                 float64(len(tagIDs)),
	}
	if hole.Good {
		features["is_good"] = 1
	}
	if hole.Locked {
		features["is_locked"] = 1
	}
	if hole.Frozen {
		features["is_frozen"] = 1
	}
	if feedback.negative[hole.ID] {
		features["feedback_negative"] = 1
	}
	if isFreshPost(hole, now) {
		features["is_fresh_post"] = 1
	}
	if freshFeature {
		features["feature_hot_score"] = feature.HotScore
		features["feature_quality_score"] = feature.QualityScore
		features["feature_controversy_score"] = feature.ControversyScore
		features["feature_reply_1h_log"] = math.Log1p(float64(feature.Reply1h))
		features["feature_reply_6h_log"] = math.Log1p(float64(feature.Reply6h))
		features["feature_view_1h_log"] = math.Log1p(float64(feature.View1h))
	}
	return features
}
