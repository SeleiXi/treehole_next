package recsys

import (
	"time"

	"github.com/opentreehole/go-common"
)

const (
	ModeClassic   = "classic"
	ModeRecommend = "recommend"

	EventImpression = "impression"
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
		normalize(sortStrategy) == ModeRecommend
}

func normalize(value string) string {
	switch value {
	case "recommend", "recommended", "feed", "recommend_feed":
		return ModeRecommend
	case "classic", "active", "time_updated", "time_created", "created_at", "updated_at", "original", "":
		return ModeClassic
	default:
		return value
	}
}
