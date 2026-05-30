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
	Strategy       string
	Order          string
	Offset         time.Time
	Size           int
	CursorScore    *float64
	CursorID       *int
	ExcludeHoleIDs []int
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
	case StrategyRecommend, "search_recommend", "search-recommend", "retrieval_rank_push":
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
	db = db.Select("hole.*, ("+expr.SQL+") AS sort_score", expr.Vars...)
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
	return db.
		Order("sort_score desc").
		Order("hole.id desc").
		Limit(opts.Size)
}

func unixSeconds(column string, dialect string) string {
	if dialect == "sqlite" {
		return "CAST(strftime('%s', " + column + ") AS REAL)"
	}
	return "UNIX_TIMESTAMP(" + column + ")"
}

func hotScore(dialect string) scoreExpr {
	updated := unixSeconds("hole.updated_at", dialect)
	return scoreExpr{
		SQL: "(" +
			"COALESCE(hole.reply, 0) * ? + " +
			"COALESCE(hole.view, 0) * ? + " +
			"COALESCE(hole.favorite_count, 0) * ? + " +
			"COALESCE(hole.subscription_count, 0) * ? + " +
			updated + " / ?" +
			")",
		Vars: []any{12.0, 0.25, 24.0, 18.0, 86400.0},
	}
}

func recommendScore(dialect string) scoreExpr {
	created := unixSeconds("hole.created_at", dialect)
	updated := unixSeconds("hole.updated_at", dialect)
	return scoreExpr{
		SQL: "(" +
			"COALESCE(hole.reply, 0) * ? + " +
			"COALESCE(hole.view, 0) * ? + " +
			"COALESCE(hole.favorite_count, 0) * ? + " +
			"COALESCE(hole.subscription_count, 0) * ? + " +
			created + " / ? + " +
			updated + " / ?" +
			")",
		Vars: []any{5.0, 0.15, 30.0, 18.0, 172800.0, 259200.0},
	}
}
