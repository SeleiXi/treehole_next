package models

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/opentreehole/go-common"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"treehole_next/config"
	"treehole_next/utils"

	"github.com/goccy/go-json"

	stdjson "encoding/json"
)

var ES *elasticHTTPClient

const IndexName = "floors"

func Init() {
	if config.Config.Mode == "test" || config.Config.Mode == "bench" || config.Config.ElasticsearchUrl == "" {
		return
	}

	// export ELASTICSEARCH_URL environment variable to set the ElasticSearch URL
	// example: http://user:pass@127.0.0.1:9200
	var err error
	ES, err = newElasticHTTPClient(config.Config.ElasticsearchUrl)
	if err != nil {
		log.Printf("error creating elasticsearch client: %s", err)
		ES = nil
		return
	}

	res, err := ES.request(context.Background(), http.MethodGet, "", nil, "", nil)
	if err != nil {
		log.Fatal().Err(err).Msg("error getting elasticsearch response")
	}
	defer closeElasticResponse(res)
	if elasticResponseIsError(res) {
		log.Fatal().Int("status", res.StatusCode).Msg("error getting elasticsearch response")
	}
	var info elasticInfoResponse
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		log.Fatal().Err(err).Msg("error decoding elasticsearch response")
	}

	// print Client and Server Info
	log.Info().Str("url", ES.baseURL.String()).Msg("elasticsearch client configured")
	//log.Info().Msgf("elasticsearch Server: %s", r["version"].(map[string]interface{})["number"])
	log.Info().Msgf("elasticsearch Server: %s\n", info.Version.Number)
	log.Info().Msgf("elasticsearch Server Minimum Index Compatibility Version: %s\n", info.Version.MinimumIndexCompatibilityVersion)
	log.Info().Msgf("elasticsearch Server Minimum Wire Compatibility Version: %s\n", info.Version.MinimumWireCompatibilityVersion)
}

type elasticHTTPClient struct {
	baseURL  *url.URL
	client   *http.Client
	username string
	password string
}

func newElasticHTTPClient(rawURL string) (*elasticHTTPClient, error) {
	baseURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("invalid elasticsearch url")
	}

	var username, password string
	if baseURL.User != nil {
		username = baseURL.User.Username()
		password, _ = baseURL.User.Password()
		baseURL.User = nil
	}

	return &elasticHTTPClient{
		baseURL:  baseURL,
		client:   &http.Client{Timeout: 30 * time.Second},
		username: username,
		password: password,
	}, nil
}

func (client *elasticHTTPClient) request(ctx context.Context, method, path string, body io.Reader, contentType string, params url.Values) (*http.Response, error) {
	endpoint := *client.baseURL
	endpoint.Path = joinElasticPath(endpoint.Path, path)
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if client.username != "" || client.password != "" {
		req.SetBasicAuth(client.username, client.password)
	}
	return client.client.Do(req)
}

func joinElasticPath(basePath, path string) string {
	if path == "" {
		if basePath == "" {
			return "/"
		}
		return basePath
	}
	if basePath == "" || basePath == "/" {
		return path
	}
	if basePath[len(basePath)-1] == '/' {
		basePath = basePath[:len(basePath)-1]
	}
	return basePath + path
}

type elasticInfoResponse struct {
	Version struct {
		Number                           string `json:"number"`
		MinimumIndexCompatibilityVersion string `json:"minimum_index_compatibility_version"`
		MinimumWireCompatibilityVersion  string `json:"minimum_wire_compatibility_version"`
	} `json:"version"`
}

type elasticSearchResponse struct {
	Hits struct {
		Hits []elasticSearchHit `json:"hits"`
	} `json:"hits"`
}

type elasticSearchHit struct {
	ID        string              `json:"_id"`
	Score     *float64            `json:"_score"`
	Highlight map[string][]string `json:"highlight"`
}

func closeElasticResponse(res *http.Response) {
	if res == nil || res.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()
}

func elasticResponseIsError(res *http.Response) bool {
	return res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices
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
	return SearchWithRequest(c, keyword, size, offset, accurate, startTime, endTime, "")
}

func SearchWithRequest(c *fiber.Ctx, keyword string, size, offset int, accurate bool, startTime *int64, endTime *int64, requestID string) (HighlightedFloors, error) {
	if requestID == "" {
		requestID = uuid.NewString()
	}
	if ES == nil {
		return searchOldWithRequest(c, keyword, size, offset, startTime, endTime, requestID, accurate)
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

	var filterQueries []map[string]any
	var disMaxQueries []map[string]any

	if accurate {
		disMaxQueries = []map[string]any{
			{"match_phrase": map[string]any{"content": map[string]any{"query": keyword}}},
			{"match_phrase": map[string]any{"content.ik_smart": map[string]any{"query": keyword}}},
		}
	} else {
		disMaxQueries = []map[string]any{
			{"match": map[string]any{"content": map[string]any{"query": keyword}}},
			{"match": map[string]any{"content.ik_smart": map[string]any{"query": keyword}}},
		}
	}

	if startTime != nil || endTime != nil {
		dateRangeQuery := map[string]any{}
		if startTime != nil {
			start := time.Unix(*startTime, 0).UTC().Format(time.RFC3339)
			dateRangeQuery["gte"] = start
		}
		if endTime != nil {
			end := time.Unix(*endTime, 0).UTC().Format(time.RFC3339)
			dateRangeQuery["lte"] = end
		}
		timeRangeQuery := map[string]any{
			"range": map[string]any{
				"updated_at": dateRangeQuery,
			},
		}
		filterQueries = append(filterQueries, timeRangeQuery)
	}

	searchBody := map[string]any{
		"from": offset,
		"size": fetchSize,
		"query": map[string]any{
			"bool": map[string]any{
				"must": []map[string]any{
					{
						"dis_max": map[string]any{
							"queries": disMaxQueries,
						},
					},
				},
				"filter": filterQueries,
			},
		},
		"highlight": map[string]any{
			"fields": map[string]any{
				"content": map[string]any{
					"number_of_fragments": 0,
				},
				"content.ik_smart": map[string]any{
					"number_of_fragments": 0,
				},
			},
			"pre_tags":  []string{HighlightBegin},
			"post_tags": []string{HighlightEnd},
		},
		"sort": []map[string]any{
			{
				"_score": map[string]any{
					"order": "desc",
				},
			},
			{
				"updated_at": map[string]any{
					"order": "desc",
				},
			},
		},
	}

	body, err := json.Marshal(searchBody)
	if err != nil {
		log.Err(err).Msg("error marshaling elasticsearch search request")
		return nil, common.InternalServerError("error preparing search request")
	}

	res, err := ES.request(
		context.Background(),
		http.MethodPost,
		"/"+IndexName+"/_search",
		bytes.NewReader(body),
		"application/json",
		nil,
	)
	if err != nil {
		log.Err(err).Msg("error searching floors")
		return nil, common.InternalServerError(fmt.Sprintf("error searching floors: %e", err))
	}
	defer closeElasticResponse(res)
	if elasticResponseIsError(res) {
		errorMsg := fmt.Sprintf("error searching floors: elasticsearch status %d", res.StatusCode)
		log.Error().
			Int("status", res.StatusCode).
			Msg("error searching floors")
		return nil, &common.HttpError{Code: res.StatusCode, Message: errorMsg}
	}

	var searchResult elasticSearchResponse
	if err := json.NewDecoder(res.Body).Decode(&searchResult); err != nil {
		log.Err(err).Msg("error decoding elasticsearch search response")
		return nil, common.InternalServerError("error decoding search response")
	}

	// get floors
	floorSize := len(searchResult.Hits.Hits)
	if floorSize == 0 {
		return HighlightedFloors{}, nil
	}
	floors := make(Floors, 0, floorSize)

	floorIDs := make([]int, floorSize)
	highlightedContents := make(map[int]string)
	baseRanks := make(map[int]int, floorSize)
	baseScores := make(map[int]*float64, floorSize)
	for i, hit := range searchResult.Hits.Hits {
		id, err := strconv.Atoi(hit.ID)
		if err != nil {
			var errorMsg = "error parsing floor_id from ElasticSearch ID"
			log.Err(err).Msg(errorMsg)
			return nil, common.InternalServerError(errorMsg)
		}
		floorIDs[i] = id
		baseRanks[id] = offset + i
		score := float64(0)
		if hit.Score != nil {
			score = *hit.Score
		}
		baseScores[id] = &score
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
	now := time.Now()
	// preprocess here to reuse Floors#Preprocess
	err = floors.Preprocess(c)
	if err != nil {
		log.Err(err).Msg("error preprocessing floors")
		return nil, err
	}
	floors = applySearchFeedbackFatigue(DB, c, floors, size, now)
	floors = applySearchModelRerank(c, keyword, floors, baseRanks, baseScores, accurate, "elastic", now)

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
	LogSearchImpressions(DB, c, keyword, accurate, "elastic", requestID, searchResultLogItems(floors, baseRanks, baseScores))

	return highlightedFloors, nil
}

// SearchOld searches floors by keyword by Database.
// It is used when ElasticSearch is not available. (Not recommended)
func SearchOld(c *fiber.Ctx, keyword string, size, offset int, startTimeUnix *int64, endTimeUnix *int64) (HighlightedFloors, error) {
	return SearchOldWithRequest(c, keyword, size, offset, startTimeUnix, endTimeUnix, "")
}

func SearchOldWithRequest(c *fiber.Ctx, keyword string, size, offset int, startTimeUnix *int64, endTimeUnix *int64, requestID string) (HighlightedFloors, error) {
	return searchOldWithRequest(c, keyword, size, offset, startTimeUnix, endTimeUnix, requestID, false)
}

func searchOldWithRequest(c *fiber.Ctx, keyword string, size, offset int, startTimeUnix *int64, endTimeUnix *int64, requestID string, accurate bool) (HighlightedFloors, error) {
	if requestID == "" {
		requestID = uuid.NewString()
	}
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
	querySet = applyDBFallbackSearchPlan(querySet)
	querySet = applySearchFeedbackQuerySort(querySet, c, now)
	err = querySet.
		Where("floor.content LIKE ?", "%"+keyword+"%").
		Where("EXISTS (SELECT 1 FROM hole WHERE hole.id = floor.hole_id AND hole.hidden = false)").
		Order("floor.id desc").Find(&floors).Error
	if err != nil {
		log.Err(err).Msgf("error finding floors by keyword '%s'", keyword)
		return nil, err
	}

	baseRanks := make(map[int]int, len(floors))
	for i, floor := range floors {
		if floor != nil {
			baseRanks[floor.ID] = offset + i
		}
	}
	floors = applySearchFeedbackFatigue(DB, c, floors, size, now)
	floors = applySearchModelRerank(c, keyword, floors, baseRanks, nil, accurate, "db", now)
	result, err := PreprocessAndHighlight(c, floors, keyword)
	if err != nil {
		return nil, err
	}
	LogSearchImpressions(DB, c, keyword, accurate, "db", requestID, searchResultLogItems(floors, baseRanks, nil))

	return result, nil
}

func searchResultLogItems(floors Floors, baseRanks map[int]int, baseScores map[int]*float64) []SearchResultLogItem {
	items := make([]SearchResultLogItem, 0, len(floors))
	for position, floor := range floors {
		if floor == nil {
			continue
		}
		baseRank, ok := baseRanks[floor.ID]
		if !ok {
			baseRank = position
		}
		var baseScore *float64
		if baseScores != nil {
			baseScore = baseScores[floor.ID]
		}
		items = append(items, SearchResultLogItem{
			FloorID:   floor.ID,
			HoleID:    floor.HoleID,
			BaseRank:  baseRank,
			BaseScore: baseScore,
		})
	}
	return items
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

func applyDBFallbackSearchPlan(querySet *gorm.DB) *gorm.DB {
	if DB.Dialector.Name() == "mysql" {
		return querySet.Table("floor FORCE INDEX(PRIMARY)")
	}
	return querySet
}

func applySearchFeedbackQuerySort(querySet *gorm.DB, c *fiber.Ctx, now time.Time) *gorm.DB {
	userID := feedbackUserID(c)
	if userID == 0 {
		return querySet
	}
	if now.IsZero() {
		now = time.Now()
	}

	var hardIDs []int
	if err := DB.Model(&FeedEvent{}).
		Where("user_id = ?", userID).
		Where("event_type IN ?", []string{FeedEventHide, FeedEventReport}).
		Where("created_at >= ?", now.Add(-feedbackNegativeLookback)).
		Distinct().
		Pluck("hole_id", &hardIDs).Error; err != nil {
		return querySet
	}
	if len(hardIDs) != 0 {
		querySet = querySet.Where("floor.hole_id NOT IN ?", hardIDs)
	}
	return querySet
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

	res, err := ES.request(
		context.Background(),
		http.MethodPost,
		"/"+IndexName+"/_bulk",
		BulkBuffer,
		"application/x-ndjson",
		nil,
	)
	if err != nil {
		log.Printf("error indexing floors %v: %s", floorIDs, err)
		return
	}
	defer closeElasticResponse(res)
	if elasticResponseIsError(res) {
		log.Error().
			Int("status", res.StatusCode).
			Ints("floor_ids", floorIDs).
			Msg("error indexing floors")
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

	res, err := ES.request(
		context.Background(),
		http.MethodPost,
		"/"+IndexName+"/_bulk",
		BulkBuffer,
		"application/x-ndjson",
		nil,
	)
	if err != nil {
		log.Printf("error deleting floors %v: %s", floorIDs, err)
		return
	}
	defer closeElasticResponse(res)
	if elasticResponseIsError(res) {
		log.Error().
			Int("status", res.StatusCode).
			Ints("floor_ids", floorIDs).
			Msg("error deleting floors")
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

	data, err := json.Marshal(floorModel)
	if err != nil {
		log.Err(err).Msg("error marshal floor")
		return
	}

	params := url.Values{}
	params.Set("refresh", "false")
	res, err := ES.request(
		context.Background(),
		http.MethodPut,
		"/"+IndexName+"/_doc/"+url.PathEscape(strconv.Itoa(floorModel.ID)),
		bytes.NewReader(data),
		"application/json",
		params,
	)

	if err != nil {
		log.Err(err).
			Msg("error index floor")
		return
	}
	defer closeElasticResponse(res)
	if elasticResponseIsError(res) {
		log.Error().
			Int("status", res.StatusCode).
			Int("floor_id", floorModel.ID).
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
	res, err := ES.request(
		context.Background(),
		http.MethodDelete,
		"/"+IndexName+"/_doc/"+url.PathEscape(strconv.Itoa(floorID)),
		nil,
		"",
		nil,
	)

	if err != nil {
		log.Err(err).
			Msg("error delete floor")
		return
	}
	defer closeElasticResponse(res)
	if elasticResponseIsError(res) {
		log.Error().
			Int("status", res.StatusCode).
			Int("floor_id", floorID).
			Msg("error delete floor")
	} else {
		log.Info().Int("floor_id", floorID).Msg("delete floor success")
	}
}
