package recsys

import (
	"math"
	"time"

	"treehole_next/models"
)

const featureMaxAge = 15 * time.Minute

type scoredHole struct {
	hole   *models.Hole
	score  float64
	tagIDs []int
}

func scoreHole(hole *models.Hole, feature *models.HoleFeature, now time.Time) float64 {
	reply24h := float64(hole.Reply)
	view24h := float64(hole.View)
	freshFeature := feature != nil && featureFreshAt(feature.UpdatedAt, now)
	if freshFeature {
		reply24h = float64(feature.Reply24h)
		view24h = float64(feature.View24h)
	}

	ageHours := math.Max(now.Sub(hole.CreatedAt).Hours(), 0)
	updateHours := math.Max(now.Sub(hole.UpdatedAt).Hours(), 0)
	freshness := 24.0 / (24.0 + updateHours)
	newPostBoost := 0.0
	if ageHours <= 48 {
		newPostBoost = 1.2
	}

	score := 2.5*math.Log1p(reply24h) +
		2.0*math.Log1p(float64(hole.FavoriteCount)) +
		1.5*math.Log1p(float64(hole.SubscriptionCount)) +
		0.4*math.Log1p(view24h) +
		3.0*freshness +
		newPostBoost

	if hole.Good {
		score += 2.0
	}
	if hole.Locked || hole.Frozen {
		score -= 1.0
	}
	if freshFeature {
		score += 0.35*feature.HotScore + 0.5*feature.QualityScore - 0.25*feature.ControversyScore
	}
	return score
}

func featureFreshAt(updatedAt time.Time, now time.Time) bool {
	if now.IsZero() {
		now = time.Now()
	}
	age := now.Sub(updatedAt)
	return age >= 0 && age <= featureMaxAge
}
