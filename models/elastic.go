package models

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/refresh"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/sortorder"
	"github.com/opentreehole/go-common"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"treehole_next/config"
	"treehole_next/utils"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/goccy/go-json"

	stdjson "encoding/json"
)

var ES *elasticsearch.TypedClient

const IndexName = "floors"

func Init() {
	if config.Config.Mode == "test" || config.Config.Mode == "bench" || config.Config.ElasticsearchUrl == "" {
		return
	}

	// export ELASTICSEARCH_URL environment variable to set the ElasticSearch URL
	// example: http://user:pass@127.0.0.1:9200
	var err error
	ES, err = elasticsearch.NewTypedClient(elasticsearch.Config{
		Addresses: []string{config.Config.ElasticsearchUrl},
	})
	if err != nil {
		log.Printf("error creating elasticsearch client: %s", err)
		ES = nil
		return
	}

	info, err := ES.Info().Do(context.Background())
	if err != nil {
		log.Fatal().Err(err).Msg("error getting elasticsearch response")
	}

	// print Client and Server Info
	log.Info().Msgf("elasticsearch Client: %s\n", elasticsearch.Version)
	//log.Info().Msgf("elasticsearch Server: %s", r["version"].(map[string]interface{})["number"])
	log.Info().Msgf("elasticsearch Server: %s\n", info.Version.Int)
	log.Info().Msgf("elasticsearch Server Minimum Index Compatibility Version: %s\n", info.Version.MinimumIndexCompatibilityVersion)
	log.Info().Msgf("elasticsearch Server Minimum Wire Compatibility Version: %s\n", info.Version.MinimumWireCompatibilityVersion)
}

type FloorModel struct {
	ID        int       `json:"id"`
	UpdatedAt time.Time `json:"updated_at"`
	Content   string    `json:"content"`
}

const HighlightBegin = "<em>"
const HighlightEnd = "</em>"
const HighlightReplace = HighlightBegin + "$0" + HighlightEnd

type HighlightedFloor struct {
	*Floor
	HighlightedContent string
}

func (floor HighlightedFloor) MarshalJSON() ([]byte, error) {
	// workaround: use stdjson to avoid go-json panicking upon flattening structs with recursive fields
	return stdjson.Marshal(&struct {
		*Floor
		HighlightedContent string `json:"highlighted_content"`
	}{
		Floor:              floor.Floor,
		HighlightedContent: floor.HighlightedContent,
	})
}

type HighlightedFloors []*HighlightedFloor

func (floors HighlightedFloors) Preprocess(_ *fiber.Ctx) error {
	// no-op intended (preprocessing done for Floors in Search and SearchOld)
	return nil
}

// Search searches floors by keyword.
//
// Parameters:
// - c: Fiber context
// - keyword: The keyword to search for
// - size: The number of results to return
// - offset: The starting point of the results
// - accurate: Whether to use accurate search
// - startTime and endTime: Filter floors by time (If not specified, set to nil)
//
// Returns:
// - HighlightedFloors: A list of floors matching the search criteria, each floor with an extra HighlightedContent field
// - error: An error if the search fails
func Search(c *fiber.Ctx, keyword string, size, offset int, accurate bool, startTime *int64, endTime *int64) (HighlightedFloors, error) {
	if ES == nil {
		return SearchOld(c, keyword, size, offset, startTime, endTime)
	}
	fetchSize := expandedSearchFetchSize(size)

	// our query design:
	// {
	// 	"query": {
	// 		"bool": {
	// 			"must": {
	// 				"dis_max": {
	// 					"queries": [{
	// 						 "multi_match": {}
	// 					 },
	// 					 {
	// 						 "multi_match": {}
	// 					 }]
	// 				}
	// 			},
	// 			"filter": {
	// 				//Term filter
	// 			}
	// 		}
	// 	}
	// }

	var filterQueries []types.Query
	var disMaxQueries []types.Query

	if accurate {
		disMaxQueries = []types.Query{
			{MatchPhrase: map[string]types.MatchPhraseQuery{"content": {Query: keyword}}},
			{MatchPhrase: map[string]types.MatchPhraseQuery{"content.ik_smart": {Query: keyword}}},
		}
	} else {
		disMaxQueries = []types.Query{
			{Match: map[string]types.MatchQuery{"content": {Query: keyword}}},
			{Match: map[string]types.MatchQuery{"content.ik_smart": {Query: keyword}}},
		}
	}

	if startTime != nil || endTime != nil {
		dateRangeQuery := types.DateRangeQuery{}
		if startTime != nil {
			start := time.Unix(*startTime, 0).UTC().Format(time.RFC3339)
			dateRangeQuery.Gte = &start
		}
		if endTime != nil {
			end := time.Unix(*endTime, 0).UTC().Format(time.RFC3339)
			dateRangeQuery.Lte = &end
		}
		timeRangeQuery := types.Query{
			Range: map[string]types.RangeQuery{
				"updated_at": dateRangeQuery,
			},
		}
		filterQueries = append(filterQueries, timeRangeQuery)
	}

	query := types.Query{
		Bool: &types.BoolQuery{
			Must: []types.Query{
				{
					DisMax: &types.DisMaxQuery{
						Queries: disMaxQueries,
					},
				},
			},
			Filter: filterQueries,
		},
	}

	highlight := &types.Highlight{
		Fields: map[string]types.HighlightField{
			"content": {
				NumberOfFragments: &[]int{0}[0],
			},
			"content.ik_smart": {
				NumberOfFragments: &[]int{0}[0],
			},
		},
		PreTags:  []string{HighlightBegin},
		PostTags: []string{HighlightEnd},
	}

	res, err := ES.Search().
		Index(IndexName).From(offset).
		Size(fetchSize).Query(&query).
		Highlight(highlight).
		Sort(
			types.SortOptions{
				SortOptions: map[string]types.FieldSort{
					"_score": {Order: &sortorder.Desc},
				},
			},
			types.SortOptions{
				SortOptions: map[string]types.FieldSort{
					"updated_at": {Order: &sortorder.Desc},
				},
			}).
		Do(context.Background())

	if err != nil {
		var errorMsg = fmt.Sprintf("error searching floors: %e", err)
		log.Err(err).Msg("error searching floors")

		var esError *types.ElasticsearchError
		if errors.As(err, &esError) {
			data, _ := json.Marshal(esError)
			log.Err(err).
				Bytes("error_detail", data).
				Msg("error searching floors")
			return nil, &common.HttpError{Code: esError.Status, Message: errorMsg}
		}
		return nil, common.InternalServerError(errorMsg)
	}

	// get floors
	floorSize := len(res.Hits.Hits)
	if floorSize == 0 {
		return HighlightedFloors{}, nil
	}
	floors := make(Floors, 0, floorSize)

	floorIDs := make([]int, floorSize)
	highlightedContents := make(map[int]string)
	for i, hit := range res.Hits.Hits {
		id, err := strconv.Atoi(*hit.Id_)
		if err != nil {
			var errorMsg = "error parsing floor_id from ElasticSearch ID"
			log.Err(err).Msg(errorMsg)
			return nil, common.InternalServerError(errorMsg)
		}
		floorIDs[i] = id
		if hit.Highlight != nil {
			var fragments []string
			if f, ok := hit.Highlight["content"]; ok && len(f) > 0 {
				fragments = f
			} else if f, ok := hit.Highlight["content.ik_smart"]; ok && len(f) > 0 {
				fragments = f
			}
			if len(fragments) > 0 {
				highlightedContents[id] = fragments[0]
			}
		}
	}
	log.Info().Ints("floor_ids", floorIDs).Msg("search response")

	querySet, err := MakeFloorQuerySet(c)
	if err != nil {
		log.Err(err).Msg("error building floor query set")
		return nil, err
	}
	err = querySet.Find(&floors, floorIDs).Error
	if err != nil {
		log.Err(err).Msgf("error finding floors by IDs: %v", floorIDs)
		return nil, err
	}

	floors = utils.OrderInGivenOrder(floors, floorIDs)
	// preprocess here to reuse Floors#Preprocess
	err = floors.Preprocess(c)
	if err != nil {
		log.Err(err).Msg("error preprocessing floors")
		return nil, err
	}
	floors = applySearchFeedbackFatigue(DB, c, floors, size, time.Now())

	highlightedFloors := make(HighlightedFloors, len(floors))
	for i, floor := range floors {
		var highlightedContent string
		// ElasticSearch is not aware of potentially sensitive content, so we replace highlighted content with the
		// sanitized one from floor.Content if the floor is sensitive
		if floor.Sensitive() && !floor.Deleted && !floor.IsMe {
			highlightedContent = floor.Content
		} else {
			if hc, ok := highlightedContents[floor.ID]; ok {
				highlightedContent = hc
			} else {
				highlightedContent = floor.Content
			}
		}
		highlightedFloors[i] = &HighlightedFloor{
			Floor:              floor,
			HighlightedContent: highlightedContent,
		}
	}

	return highlightedFloors, nil
}

// SearchOld searches floors by keyword by Database.
// It is used when ElasticSearch is not available. (Not recommended)
func SearchOld(c *fiber.Ctx, keyword string, size, offset int, startTimeUnix *int64, endTimeUnix *int64) (HighlightedFloors, error) {
	floors := Floors{}
	fetchSize := expandedSearchFetchSize(size)
	var startTime, endTime *time.Time
	if startTimeUnix != nil {
		start := time.Unix(*startTimeUnix, 0)
		startTime = &start
	}
	if endTimeUnix != nil {
		end := time.Unix(*endTimeUnix, 0)
		endTime = &end
	}
	querySet, err := floors.MakeQuerySetWithTimeRange(nil, &offset, &fetchSize, startTime, endTime, c)
	if err != nil {
		log.Err(err).Msg("error building floor query set with time range")
		return nil, err
	}

	now := time.Now()
	querySet = applySearchFeedbackQuerySort(querySet, c, now)
	err = querySet.
		Where("content like ?", "%"+keyword+"%").
		Where("hole_id in (?)", DB.Table("hole").Select("id").Where("hidden = false")).
		Order("id desc").Find(&floors).Error
	if err != nil {
		log.Err(err).Msgf("error finding floors by keyword '%s'", keyword)
		return nil, err
	}

	floors = applySearchFeedbackFatigue(DB, c, floors, size, time.Now())
	result, err := PreprocessAndHighlight(c, floors, keyword)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func expandedSearchFetchSize(size int) int {
	if size <= 0 {
		return size
	}
	fetchSize := size * 4
	if fetchSize < 20 {
		fetchSize = 20
	}
	if fetchSize > 200 {
		fetchSize = 200
	}
	return fetchSize
}

func applySearchFeedbackQuerySort(querySet *gorm.DB, c *fiber.Ctx, now time.Time) *gorm.DB {
	userID := feedbackUserID(c)
	if userID == 0 {
		return querySet
	}
	if now.IsZero() {
		now = time.Now()
	}

	querySet = querySet.Where(
		"NOT EXISTS (SELECT 1 FROM feed_event fe WHERE fe.user_id = ? AND fe.hole_id = floor.hole_id AND fe.event_type IN ('hide', 'report') AND fe.created_at >= ?)",
		userID,
		now.Add(-feedbackNegativeLookback),
	)
	return querySet.Order(clause.Expr{
		SQL: "CASE WHEN EXISTS (SELECT 1 FROM feed_event fe WHERE fe.user_id = ? AND fe.hole_id = floor.hole_id AND fe.event_type IN ('open', 'click') AND fe.created_at >= ?) THEN 1 ELSE 0 END ASC",
		Vars: []any{
			userID,
			now.Add(-feedbackOpenLookback),
		},
	})
}

func applySearchFeedbackFatigue(tx *gorm.DB, c *fiber.Ctx, floors Floors, limit int, now time.Time) Floors {
	if len(floors) == 0 {
		return floors
	}
	holeIDs := make([]int, 0, len(floors))
	seen := map[int]bool{}
	for _, floor := range floors {
		if floor == nil || floor.HoleID == 0 || seen[floor.HoleID] {
			continue
		}
		seen[floor.HoleID] = true
		holeIDs = append(holeIDs, floor.HoleID)
	}
	suppression := LoadHoleFeedbackSuppression(tx, c, holeIDs, now)
	if len(suppression.HardIDs) == 0 && len(suppression.SoftIDs) == 0 {
		return trimSearchFloors(floors, limit)
	}

	fresh := make(Floors, 0, len(floors))
	soft := make(Floors, 0)
	for _, floor := range floors {
		if floor == nil || suppression.Hard[floor.HoleID] {
			continue
		}
		if suppression.Soft[floor.HoleID] {
			soft = append(soft, floor)
			continue
		}
		fresh = append(fresh, floor)
	}
	fresh = append(fresh, soft...)
	return trimSearchFloors(fresh, limit)
}

func trimSearchFloors(floors Floors, limit int) Floors {
	if limit >= 0 && len(floors) > limit {
		return floors[:limit]
	}
	return floors
}

func PreprocessAndHighlight(c *fiber.Ctx, floors Floors, keyword string) (HighlightedFloors, error) {
	// preprocess here to reuse Floors#Preprocess
	err := floors.Preprocess(c)
	if err != nil {
		log.Err(err).Msg("error preprocessing floors")
		return nil, err
	}

	// at this point potentially sensitive content is wiped out by Preprocess, so we can highlight safely
	highlighted := make(HighlightedFloors, len(floors))

	// skip highlighting if keyword is empty
	if keyword == "" {
		CopyToHighlightedFloors(floors, highlighted)
		return highlighted, nil
	}

	// (?i) for case insensitivity, QuoteMeta to avoid regex injection
	regex := "(?i)" + regexp.QuoteMeta(keyword)
	pattern, err := regexp.Compile(regex)
	if err != nil {
		log.Err(err).Msgf("error compiling highlight regex: '%s'", regex)
		// fall back to unhighlighted result
		CopyToHighlightedFloors(floors, highlighted)
		return highlighted, nil
	}

	for i, floor := range floors {
		highlighted[i] = &HighlightedFloor{
			Floor:              floor,
			HighlightedContent: pattern.ReplaceAllString(floor.Content, HighlightReplace),
		}
	}

	return highlighted, nil
}

// CopyToHighlightedFloors converts Floors to HighlightedFloors but doesn't actually perform the highlighting
func CopyToHighlightedFloors(floors Floors, result HighlightedFloors) {
	for i, floor := range floors {
		result[i] = &HighlightedFloor{
			Floor:              floor,
			HighlightedContent: floor.Content,
		}
	}
}

// BulkInsert run in single goroutine only
// see https://www.elastic.co/guide/en/elasticsearch/reference/master/docs-bulk.html
func BulkInsert(floors []FloorModel) {
	if ES == nil {
		return
	}
	if len(floors) == 0 {
		return
	}

	var BulkBuffer = bytes.NewBuffer(make([]byte, 0, 1024000)) // 100 KB buffer

	for _, floor := range floors {
		// meta: use index, it will insert or replace a document
		BulkBuffer.WriteString(fmt.Sprintf(`{ "index" : { "_id" : "%d" } }%s`, floor.ID, "\n"))

		// data: should not contain \n, because \n is the delimiter of one action
		data, err := json.Marshal(floor)
		if err != nil {
			log.Printf("error failed to marshal floor: %s", err)
			return
		}
		BulkBuffer.Write(data)
		BulkBuffer.WriteByte('\n') // the final line of data must end with a newline character \n
	}

	var floorIDs []int
	for _, floorModel := range floors {
		floorIDs = append(floorIDs, floorModel.ID)
	}
	log.Info().Ints("floor_ids", floorIDs).Msg("Preparing insert floors")

	_, err := ES.Bulk().Index(IndexName).Raw(BulkBuffer).Do(context.Background())
	if err != nil {
		log.Printf("error indexing floors %v: %s", floorIDs, err)
		return
	}
	log.Info().Ints("floor_ids", floorIDs).Msg("index floors success")
}

// BulkDelete used when a hole becomes hidden and delete all of its floors
func BulkDelete(floorIDs []int) {
	if ES == nil {
		return
	}
	if len(floorIDs) == 0 {
		return
	}

	var BulkBuffer = bytes.NewBuffer(make([]byte, 0, 1024000)) // 100 KB buffer

	for _, floorID := range floorIDs {
		// meta: use index, it will insert or replace a document
		BulkBuffer.WriteString(fmt.Sprintf(`{ "delete" : { "_id" : "%d" } }%s`, floorID, "\n"))
	}
	log.Info().Ints("floor_ids", floorIDs).Msg("Preparing delete floors")

	_, err := ES.Bulk().
		Index(IndexName).
		Raw(BulkBuffer).
		Do(context.Background())
	if err != nil {
		log.Printf("error deleting floors %v: %s", floorIDs, err)
		return
	}
	log.Info().Ints("floor_ids", floorIDs).Msg("delete floors success")
}

// FloorIndex insert or replace a document, used when a floor is created or restored
// see https://www.elastic.co/guide/en/elasticsearch/reference/master/docs-index_.html
func FloorIndex(floorModel FloorModel) {
	if ES == nil {
		return
	}

	_, err := ES.
		Index(IndexName).
		Id(strconv.Itoa(floorModel.ID)).
		Document(&floorModel).
		Refresh(refresh.Refresh{Name: "false"}).
		Do(context.Background())

	if err != nil {
		log.Err(err).
			Msg("error index floor")
	} else {
		log.Info().Int("floor_id", floorModel.ID).Msg("index floor success")
	}
}

// FloorDelete used when a floor is deleted
func FloorDelete(floorID int) {
	if ES == nil {
		return
	}
	_, err := ES.Delete(
		IndexName,
		strconv.Itoa(floorID)).Do(context.Background())

	if err != nil {
		log.Err(err).
			Msg("error delete floor")
	} else {
		log.Info().Int("floor_id", floorID).Msg("delete floor success")
	}
}
