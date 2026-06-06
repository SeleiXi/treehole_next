package tests

import (
	"strconv"
	"testing"

	"github.com/rs/zerolog/log"

	. "treehole_next/models"

	"github.com/stretchr/testify/assert"
)

func TestGetReport(t *testing.T) {
	reportID := REPORT_BASE_ID
	var report Report
	DB.First(&report, reportID)
	log.Info().Any("report", report).Send()

	var getReport Report
	testAPIModel(t, "get", "/api/reports/"+strconv.Itoa(reportID), 200, &getReport)
	assert.EqualValues(t, report.FloorID, getReport.FloorID)
	assert.EqualValues(t, report.FloorID, getReport.Floor.FloorID)
}

func TestListReport(t *testing.T) {
	data := Map{}

	var getReports Reports
	testAPIModelWithQuery(t, "get", "/api/reports", 200, &getReports, data)
	log.Printf("getReports: %+v\n", getReports)

	data = Map{"range": 1}
	testAPIModelWithQuery(t, "get", "/api/reports", 200, &getReports, data)
	log.Printf("getReports: %+v\n", getReports)

	data = Map{"range": 2}
	testAPIModelWithQuery(t, "get", "/api/reports", 200, &getReports, data)
	log.Printf("getReports: %+v\n", getReports)
}

func TestAddReport(t *testing.T) {
	data := Map{"floor_id": REPORT_FLOOR_BASE_ID + 14, "reason": "123456789"}

	testAPI(t, "post", "/api/reports", 204, data)
}

func TestAddReportLogsBodyRequestID(t *testing.T) {
	requestID := "test-report-body-request"
	data := Map{"floor_id": REPORT_FLOOR_BASE_ID + 15, "reason": "123456789", "request_id": requestID}

	testAPI(t, "post", "/api/reports", 204, data)

	var count int64
	err := DB.Model(&FeedEvent{}).
		Where("request_id = ? AND event_type = ? AND hole_id = ?", requestID, FeedEventReport, 1000).
		Count(&count).Error
	assert.Nil(t, err)
	assert.EqualValues(t, 1, count)
}

func TestDeleteReport(t *testing.T) {
	reportID := REPORT_BASE_ID + 7
	var getReport Report
	data := Map{"result": "123456789"}
	testAPI(t, "delete", "/api/reports/"+strconv.Itoa(reportID), 200, data)

	DB.First(&getReport, reportID)
	assert.EqualValues(t, true, getReport.Dealt)
}
