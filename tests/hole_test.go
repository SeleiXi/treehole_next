package tests

import (
	"strconv"
	"strings"
	"testing"

	. "treehole_next/config"
	. "treehole_next/models"
	"treehole_next/recsys"
	"treehole_next/utils"

	"github.com/stretchr/testify/assert"
)

func TestListHoleInADivision(t *testing.T) {
	var holes Holes
	var ids []int

	DB.Raw("SELECT id FROM hole WHERE division_id = 6 AND hidden = 0 ORDER BY updated_at DESC").Scan(&ids)

	testAPIModel(t, "get", "/api/divisions/6/holes", 200, &holes)
	assert.Equal(t, ids[:Config.HoleFloorSize], utils.Models2IDSlice(holes))

	testAPIModel(t, "get", "/api/divisions/"+strconv.Itoa(largeInt)+"/holes", 200, &holes)        // return empty holes
	testAPI(t, "get", "/api/divisions/"+strings.Repeat(strconv.Itoa(largeInt), 15)+"/holes", 500) // huge divisionID
}

func TestListHolesByScoreSort(t *testing.T) {
	err := DB.Model(&Hole{}).Where("id = ?", 11).Updates(Map{
		"reply":              100,
		"view":               1000,
		"favorite_count":     5,
		"subscription_count": 3,
	}).Error
	assert.Nil(t, err)

	var holes Holes
	testAPIModelWithQuery(t, "get", "/api/holes", 200, &holes, Map{
		"sort_strategy": "hot",
		"length":        3,
	})
	assert.NotEmpty(t, holes)
	assert.Len(t, holes, 3)
	assert.Equal(t, 11, holes[0].ID)
	assert.NotNil(t, holes[0].SortScore)

	var nextPage Holes
	testAPIModelWithQuery(t, "get", "/api/holes", 200, &nextPage, Map{
		"sort_strategy": "hot",
		"length":        3,
		"cursor_score":  *holes[0].SortScore,
		"cursor_id":     holes[0].ID,
	})
	assert.NotEmpty(t, nextPage)
	assert.Len(t, nextPage, 3)
	assert.NotEqual(t, holes[0].ID, nextPage[0].ID)

	var recommended Holes
	testAPIModelWithQuery(t, "get", "/api/holes", 200, &recommended, Map{
		"sort_strategy": "recommend",
		"length":        3,
	})
	assert.NotEmpty(t, recommended)
	assert.Len(t, recommended, 3)
	assert.NotNil(t, recommended[0].SortScore)
}

func TestListHomePageRecommendFeed(t *testing.T) {
	var holes Holes
	testAPIModelWithQuery(t, "get", "/api/holes/_homepage", 200, &holes, Map{
		"order":      "recommend",
		"length":     5,
		"request_id": "test-feed-request",
	})
	assert.NotEmpty(t, holes)
	assert.LessOrEqual(t, len(holes), 5)
	assert.NotNil(t, holes[0].SortScore)

	var events []FeedEvent
	err := DB.Where("request_id = ?", "test-feed-request").Find(&events).Error
	assert.Nil(t, err)
	assert.Len(t, events, len(holes))

	testAPIModel(t, "get", "/api/holes/"+strconv.Itoa(holes[0].ID), 200, &Hole{})

	var openEvents []FeedEvent
	err = DB.Where("hole_id = ? AND event_type = ?", holes[0].ID, FeedEventOpen).Find(&openEvents).Error
	assert.Nil(t, err)
	assert.NotEmpty(t, openEvents)

	var refreshed Holes
	testAPIModelWithQuery(t, "get", "/api/holes/_homepage", 200, &refreshed, Map{
		"order":      "recommend",
		"length":     5,
		"request_id": "test-feed-after-open",
	})
	assert.NotEmpty(t, refreshed)
}

func TestListHomePageRecommendFeedReturnsGeneratedRequestID(t *testing.T) {
	var holes Holes
	res := testAPIModelWithQueryResponse(t, "get", "/api/holes/_homepage", 200, &holes, Map{
		"order":  "recommend",
		"length": 3,
	})
	assert.NotEmpty(t, holes)

	requestID := res.Header.Get(utils.RequestIDHeader)
	assert.NotEmpty(t, requestID)
	assert.Equal(t, requestID, res.Header.Get("X-Request-ID"))

	var count int64
	err := DB.Model(&FeedEvent{}).Where("request_id = ?", requestID).Count(&count).Error
	assert.Nil(t, err)
	assert.EqualValues(t, len(holes), count)
}

func TestFeedbackAwareScoreSortSuppressesExplicitNegatives(t *testing.T) {
	var holes Holes
	testAPIModelWithQuery(t, "get", "/api/holes", 200, &holes, Map{
		"sort_strategy": "hot",
		"length":        3,
		"request_id":    "test-hot-before-click",
	})
	assert.NotEmpty(t, holes)

	testAPIModel(t, "get", "/api/holes/"+strconv.Itoa(holes[0].ID), 200, &Hole{})
	recsys.LogEvent(nil, DB, holes[0].ID, FeedEventReport, recsys.ModeClassic, -1, "test-hot-report")

	var afterReport Holes
	testAPIModelWithQuery(t, "get", "/api/holes", 200, &afterReport, Map{
		"sort_strategy": "hot",
		"length":        3,
		"request_id":    "test-hot-after-report",
	})
	assert.NotContains(t, utils.Models2IDSlice(afterReport), holes[0].ID)
}

func TestFeedbackAwareScoreSortFatiguesOpenedHoles(t *testing.T) {
	targetIDs := []int{11, 12, 13}
	err := DB.Where("hole_id IN ?", targetIDs).Delete(&FeedEvent{}).Error
	assert.Nil(t, err)
	err = DB.Model(&Hole{}).Where("id IN ?", targetIDs).Updates(Map{
		"reply":              0,
		"view":               0,
		"favorite_count":     0,
		"subscription_count": 0,
	}).Error
	assert.Nil(t, err)
	err = DB.Model(&Hole{}).Where("id = ?", 11).Updates(Map{
		"reply":              100,
		"view":               1000,
		"favorite_count":     5,
		"subscription_count": 3,
	}).Error
	assert.Nil(t, err)
	err = DB.Model(&Hole{}).Where("id = ?", 12).Updates(Map{
		"reply": 20,
		"view":  200,
	}).Error
	assert.Nil(t, err)

	var beforeOpen Holes
	testAPIModelWithQuery(t, "get", "/api/holes", 200, &beforeOpen, Map{
		"sort_strategy": "hot",
		"length":        5,
		"request_id":    "test-hot-before-open-fatigue",
	})
	assert.NotEmpty(t, beforeOpen)
	assert.Equal(t, 11, beforeOpen[0].ID)

	testAPIModel(t, "get", "/api/holes/11", 200, &Hole{})

	var afterOpen Holes
	testAPIModelWithQuery(t, "get", "/api/holes", 200, &afterOpen, Map{
		"sort_strategy": "hot",
		"length":        5,
		"request_id":    "test-hot-after-open-fatigue",
	})
	assert.NotEmpty(t, afterOpen)
	assert.NotEqual(t, 11, afterOpen[0].ID)
}

func TestListHolesByTag(t *testing.T) {
	var tag Tag
	DB.Where("name = ?", "114").First(&tag)
	var holes Holes
	err := DB.Model(&tag).Association("Holes").Find(&holes)
	if err != nil {
		t.Fatal(err)
	}

	var getHoles Holes
	testAPIModel(t, "get", "/api/tags/114/holes", 200, &getHoles)
	assert.EqualValues(t, len(holes), len(getHoles))

	// empty holes
	testAPIModel(t, "get", "/api/tags/115/holes", 200, &getHoles)
	assert.EqualValues(t, Holes{}, getHoles)
}

func TestCreateHole(t *testing.T) {
	content := "abcdef"
	data := Map{"content": content, "tags": []Map{{"name": "a"}, {"name": "ab"}, {"name": "abc"}}}
	testAPI(t, "post", "/api/divisions/1/holes", 201, data)
	data["tags"] = []Map{{"name": "abcd"}, {"name": "ab"}, {"name": "abc"}} // update temperature or create tag
	testAPI(t, "post", "/api/divisions/1/holes", 201, data)

	tag := Tag{}
	DB.Where("name = ?", "a").First(&tag)
	assert.EqualValues(t, 1, tag.Temperature)
	tag = Tag{}
	DB.Where("name = ?", "abc").First(&tag)
	assert.EqualValues(t, 2, tag.Temperature)
	assert.EqualValues(t, 2, DB.Model(&tag).Association("Holes").Count())

	data = Map{"content": content, "tags": []Map{}}
	testAPI(t, "post", "/api/divisions/1/holes", 400, data) // at least one tag

	content = strings.Repeat("~", 15001)
	data = Map{"content": content, "tags": []Map{{"name": "a"}, {"name": "ab"}, {"name": "abc"}}}
	testAPI(t, "post", "/api/divisions/1/holes", 400, data) // data no more than 10000

	tags := make([]Map, 11)
	for i := range tags {
		tags[i] = Map{"name": strconv.Itoa(i)}
	}
	data = Map{"content": "123456789", "tags": tags} // at most 10 tags
	testAPI(t, "post", "/api/divisions/1/holes", 400, data)
}

func TestCreateHoleOld(t *testing.T) {
	content := "abcdef"
	tagName := []Map{{"name": "d"}, {"name": "de"}, {"name": "def"}}
	division_id := 1
	data := Map{"content": content, "tags": tagName, "division_id": division_id}
	testAPI(t, "post", "/api/holes", 201, data)
	tagName = []Map{{"name": "abc"}, {"name": "defg"}, {"name": "de"}}
	data = Map{"content": content, "tags": tagName, "division_id": division_id}
	testAPI(t, "post", "/api/holes", 201, data)

	var holes Holes
	var tag Tag
	DB.Where("name = ?", "def").First(&tag)
	err := DB.Model(&tag).Association("Holes").Find(&holes)
	if err != nil {
		t.Fatal(err)
	}
}

func TestModifyHole(t *testing.T) {
	var tag Tag
	DB.Where("name = ?", "111").First(&tag)
	var holes Holes
	err := DB.Model(&tag).Association("Holes").Find(&holes)
	if err != nil {
		t.Fatal(err)
	}

	tagName := []Map{{"name": "111"}, {"name": "d"}, {"name": "de"}, {"name": "def"}}
	division_id := 5
	data := Map{"tags": tagName, "division_id": division_id}
	testAPI(t, "put", "/api/holes/"+strconv.Itoa(holes[0].ID), 200, data)

	DB.Preload("Tags").Where("id = ?", holes[0].ID).Find(&holes[0])

	var getTagName []Map
	for _, v := range holes[0].Tags {
		getTagName = append(getTagName, Map{"name": v.Name})
	}
	assert.EqualValues(t, tagName, getTagName)
	assert.EqualValues(t, division_id, holes[0].DivisionID)

	// default schemas
	testAPI(t, "put", "/api/holes/"+strconv.Itoa(holes[0].ID), 400, Map{}) // bad request if modify nothing
	DB.Where("id = ?", holes[0].ID).Find(&holes[0])
	assert.Equal(t, division_id, holes[0].DivisionID)
}

func TestDeleteHole(t *testing.T) {
	var hole Hole
	holeID := 10
	testAPI(t, "delete", "/api/holes/"+strconv.Itoa(holeID), 204)
	testAPI(t, "delete", "/api/holes/"+strconv.Itoa(largeInt), 404)
	DB.Where("id = ?", 10).Find(&hole)
	assert.Equal(t, true, hole.Hidden)
}

func TestHoleStats(t *testing.T) {

	for i := 1; i <= 10; i++ {
		var hole Hole
		testAPIModel(t, "get", "/api/holes/"+strconv.Itoa(i), 200, &hole)
		hole.RecalculateStats()
		assert.Equal(t, 1, hole.FavoriteCount)
	}

}
