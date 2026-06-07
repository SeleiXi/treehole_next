package ranking

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	StrategyOriginal  = "original"
	StrategyHot       = "hot"
	StrategyRecommend = "recommend"
)

type Options struct {
	Strategy           string
	Order              string
	Offset             time.Time
	Size               int
	CursorScore        *float64
	CursorID           *int
	ExcludeHoleIDs     []int
	SoftFatigueHoleIDs []int
}

type scoreExpr struct {
	SQL  string
	Vars []any
}

func NormalizeStrategy(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case "", StrategyOriginal, "default", "time":
		return StrategyOriginal
	case StrategyHot, "hotness":
		return StrategyHot
	case StrategyRecommend, "search_recommend", "search-recommend", "model_recommend", "model-recommend", "retrieval_rank_push":
		return StrategyRecommend
	default:
		return StrategyOriginal
	}
}

func Apply(db *gorm.DB, opts Options, dialect string) *gorm.DB {
	if opts.Size <= 0 {
		opts.Size = 10
	}

	switch NormalizeStrategy(opts.Strategy) {
	case StrategyHot:
		return applyScoreSort(db, hotScore(dialect), opts)
	case StrategyRecommend:
		return applyScoreSort(db, recommendScore(dialect), opts)
	default:
		return applyOriginalSort(db, opts)
	}
}

func applyOriginalSort(db *gorm.DB, opts Options) *gorm.DB {
	if opts.Order == "time_created" || opts.Order == "created_at" {
		return db.
			Where("hole.created_at < ?", opts.Offset).
			Order("hole.created_at desc").
			Limit(opts.Size)
	}
	return db.
		Where("hole.updated_at < ?", opts.Offset).
		Order("hole.updated_at desc").
		Limit(opts.Size)
}

func applyScoreSort(db *gorm.DB, expr scoreExpr, opts Options) *gorm.DB {
	if len(opts.SoftFatigueHoleIDs) != 0 {
		selectVars := append([]any{opts.SoftFatigueHoleIDs}, expr.Vars...)
		db = db.Select("hole.*, (CASE WHEN hole.id IN ? THEN 1 ELSE 0 END) AS feedback_fatigue, ("+expr.SQL+") AS sort_score", selectVars...)
	} else {
		db = db.Select("hole.*, ("+expr.SQL+") AS sort_score", expr.Vars...)
	}
	if len(opts.ExcludeHoleIDs) != 0 {
		db = db.Where("hole.id NOT IN ?", opts.ExcludeHoleIDs)
	}
	if opts.CursorScore != nil && opts.CursorID != nil {
		whereVars := append([]any{}, expr.Vars...)
		whereVars = append(whereVars, *opts.CursorScore)
		whereVars = append(whereVars, expr.Vars...)
		whereVars = append(whereVars, *opts.CursorScore, *opts.CursorID)
		db = db.Where("("+expr.SQL+") < ? OR (ABS(("+expr.SQL+") - ?) < 0.000001 AND hole.id < ?)", whereVars...)
	}
	if len(opts.SoftFatigueHoleIDs) != 0 {
		db = db.Order("feedback_fatigue asc")
	}
	return db.
		Order("sort_score desc").
		Order("hole.id desc").
		Limit(opts.Size)
}

func ageHours(column string, dialect string) string {
	if dialect == "sqlite" {
		return "MAX((CAST(strftime('%s', 'now') AS REAL) - CAST(strftime('%s', " + column + ") AS REAL)) / 3600.0, 0)"
	}
	return "GREATEST(TIMESTAMPDIFF(HOUR, " + column + ", NOW(3)), 0)"
}

func recencyBoost(column string, dialect string) string {
	age := ageHours(column, dialect)
	return "(? / (? + " + age + ") * ?)"
}

func hotScore(dialect string) scoreExpr {
	recency := recencyBoost("hole.updated_at", dialect)
	return scoreExpr{
		SQL: "(" +
			"COALESCE(hole.reply, 0) * ? + " +
			"COALESCE(hole.view, 0) * ? + " +
			"COALESCE(hole.favorite_count, 0) * ? + " +
			"COALESCE(hole.subscription_count, 0) * ? + " +
			recency +
			")",
		Vars: []any{12.0, 0.25, 24.0, 18.0, 24.0, 24.0, 8.0},
	}
}

func recommendScore(dialect string) scoreExpr {
	createdRecency := recencyBoost("hole.created_at", dialect)
	updatedRecency := recencyBoost("hole.updated_at", dialect)
	return scoreExpr{
		SQL: "(" +
			"COALESCE(hole.reply, 0) * ? + " +
			"COALESCE(hole.view, 0) * ? + " +
			"COALESCE(hole.favorite_count, 0) * ? + " +
			"COALESCE(hole.subscription_count, 0) * ? + " +
			createdRecency + " + " +
			updatedRecency +
			")",
		Vars: []any{5.0, 0.15, 30.0, 18.0, 24.0, 24.0, 4.0, 24.0, 24.0, 6.0},
	}
}
