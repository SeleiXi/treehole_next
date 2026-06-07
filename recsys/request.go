package recsys

import (
	"time"

	"github.com/opentreehole/go-common"
)

const (
	ModeClassic        = "classic"
	ModeRecommend      = "recommend"
	ModeModelRecommend = "model_recommend"
)

type HomeFeedRequest struct {
	ExcludeDivisionIDs *[]int
	Size               int
	Offset             common.CustomTime
	Tags               []string
	Order              string
	FeedMode           string
	SortStrategy       string
	CursorScore        *float64
	CursorID           *int
	CreatedStart       *common.CustomTime
	CreatedEnd         *common.CustomTime
	RequestID          string
	Now                time.Time
}

func (r HomeFeedRequest) PageSize() int {
	if r.Size > 0 {
		return r.Size
	}
	return 10
}

func ShouldUseRecommend(feedMode, order, sortStrategy string) bool {
	return normalize(feedMode) == ModeRecommend ||
		normalize(order) == ModeRecommend ||
		normalize(sortStrategy) == ModeRecommend ||
		normalize(feedMode) == ModeModelRecommend ||
		normalize(order) == ModeModelRecommend ||
		normalize(sortStrategy) == ModeModelRecommend
}

func IsFeedbackAwareRank(sortStrategy string) bool {
	switch normalize(sortStrategy) {
	case ModeRecommend, ModeModelRecommend, "hot", "hotness":
		return true
	default:
		return false
	}
}

func ShouldUseModelRank(feedMode, order, sortStrategy string) bool {
	return normalize(feedMode) == ModeModelRecommend ||
		normalize(order) == ModeModelRecommend ||
		normalize(sortStrategy) == ModeModelRecommend
}

func normalize(value string) string {
	switch value {
	case "recommend", "recommended", "feed", "recommend_feed":
		return ModeRecommend
	case "model_recommend", "model-recommend", "model", "model_feed", "model-recommend-feed":
		return ModeModelRecommend
	case "classic", "active", "time_updated", "time_created", "created_at", "updated_at", "original", "":
		return ModeClassic
	default:
		return value
	}
}
