package ga

import (
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/analytics/v3"

	"github.com/affinity226/gabeat/config"

	// optional, this is just a logger
	"github.com/elastic/beats/libbeat/logp"

	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var errorResult = GABeatDataPoint{-1, "error", "error", nil}
var errorResults = []GABeatDataPoint{errorResult}
var emptyResult = GABeatDataPoint{0, "empty", "empty", nil}
var emptyResults = []GABeatDataPoint{emptyResult}
var alphanumericPlusUnderscoreRegex, _ = regexp.Compile("[_0-9A-Za-z]")
var whitespaceRegex, _ = regexp.Compile("[[:space:]]")

const logSelector = "gahelper"

type GABeatDataPoint struct {
	Value         int
	DimensionName string
	MetricName    string
	Data          map[string]interface{}
}

type gaDataRetriever func(gaIds string, gaMetrics string, gaDimensions string) (gaData *analytics.RealtimeData, err error)

// https://developers.google.com/accounts/docs/OAuth2ServiceAccount
// Explains OAuth2 Service Account flow and how code works
func GetGAReportData(gaConfig config.GoogleAnalyticsConfig) (GAData []GABeatDataPoint, err error) {
	return getGAReportData(gaConfig, getGAData)
}

func getGAReportData(gaConfig config.GoogleAnalyticsConfig, gaDataRetrieverFunc gaDataRetriever) (GAData []GABeatDataPoint, err error) {
	if _, pathErr := initCredentialsPath(gaConfig); pathErr != nil {
		return errorResults, pathErr
	}

	if _, idsErr := validateConfig(
		gaConfig.GoogleAnalyticsIDs, "google analytics IDs"); idsErr != nil {
		return errorResults, idsErr
	}

	if _, metricErr := validateConfig(
		gaConfig.GoogleAnalyticsMetrics, "google analytics metrics"); metricErr != nil {
		return errorResults, metricErr
	}

	if _, dimensionErr := validateConfig(
		gaConfig.GoogleAnalyticsDimensions, "google analytics demensions"); dimensionErr != nil {
		return errorResults, dimensionErr
	}

	if gaConfig.GoogleAnalyticsType == "gaservice" {
		gaData, gaDataErr := getGADataForCharge(gaConfig.GoogleAnalyticsIDs,
			gaConfig.GoogleAnalyticsMetrics,
			gaConfig.GoogleAnalyticsDimensions,
			gaConfig.GoogleAnalyticsStarttime,
			gaConfig.GoogleAnalyticsEndtime,
		)
		if gaDataErr != nil {
			return errorResults, fmt.Errorf("Could not get Google Analytics data: %v", gaDataErr)
		}
		diLen := len(strings.Split(gaConfig.GoogleAnalyticsDimensions, ","))
		meLen := len(strings.Split(gaConfig.GoogleAnalyticsMetrics, ","))
		return parseGAResponseForCharge(gaData, diLen, meLen)
	} else {
		gaData, gaDataErr := gaDataRetrieverFunc(gaConfig.GoogleAnalyticsIDs,
			gaConfig.GoogleAnalyticsMetrics,
			gaConfig.GoogleAnalyticsDimensions)
		if gaDataErr != nil {
			return errorResults, fmt.Errorf("Could not get Google Analytics data: %v", gaDataErr)
		}
		gaDataPoints, parseErr := parseGAResponse(gaData)

		return gaDataPoints, parseErr
	}
}

func initCredentialsPath(gaConfig config.GoogleAnalyticsConfig) (GAData []GABeatDataPoint, err error) {
	if gaConfig.GoogleCredentialsFilePath == "" {
		credsErr := errors.New("Google credentails file must not be empty")
		return errorResults, credsErr
	}
	_, pathErr := os.Stat(gaConfig.GoogleCredentialsFilePath)
	if pathErr != nil {
		return errorResults, fmt.Errorf("Error reading google credentials file: %v", pathErr)
	}
	//The Google Analytics SDK expects to find the location of the default
	//credentials file in this env var.
	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", gaConfig.GoogleCredentialsFilePath)
	return emptyResults, nil
}

func validateConfig(configValue string,
	configName string) (GAData []GABeatDataPoint, err error) {
	if configValue == "" {
		configErr := fmt.Errorf("Config value %s must not be empty", configName)
		return errorResults, configErr
	}
	return emptyResults, nil
}

func getGAData(gaIds string, gaMetrics string, gaDimensions string) (gaData *analytics.RealtimeData, err error) {
	oauthHttpClient, err := google.DefaultClient(oauth2.NoContext, analytics.AnalyticsReadonlyScope)
	if err != nil {
		return nil, fmt.Errorf("Error creating auth context: %v", err)
	}
	analyticsService, err := analytics.New(oauthHttpClient)

	if err != nil {
		return nil, fmt.Errorf("Error creating Google Analytics client: %v", err)
	}

	dataGAService := analytics.NewDataRealtimeService(analyticsService)

	dataGAGetCall := dataGAService.Get(gaIds, gaMetrics)

	realtimeData, gaDataErr := dataGAGetCall.Dimensions(gaDimensions).Do()

	return realtimeData, gaDataErr
}

func parseGAResponse(gaData *analytics.RealtimeData) (GAData []GABeatDataPoint, err error) {
	debugGAResponse(gaData)
	if len(gaData.Rows) < 1 || len(gaData.Rows[0]) < 1 {
		return emptyResults, nil
	}
	gaDataPoints := []GABeatDataPoint{}
	metricNameHeader := getMetric(gaData)

	for _, row := range gaData.Rows {
		//ASSUMPTION: last element in row is the numerical value and all
		//preceding values are dimension names.
		var dimensionNames []string = row[0 : len(row)-1]
		dimensionName := strings.Join(dimensionNames, "_")
		var metricValue string = row[len(row)-1]
		dataPoint, err := strconv.Atoi(metricValue)
		if err != nil {
			return errorResults, fmt.Errorf("Error converting string to int: %s, %v", metricValue, err)
		}
		gaDataPoint := GABeatDataPoint{dataPoint,
			format(dimensionName),
			format(metricNameHeader),
			nil}
		gaDataPoints = append(gaDataPoints, gaDataPoint)
	}
	return gaDataPoints, nil
}

//We probably want to remove all non-alphanumeric characters from metric
//and dimension names.  Also, Elastic Beats naming conventions suggest using
//underscore to separate terms:
//https://www.elastic.co/guide/en/beats/libbeat/current/event-conventions.html
func format(toSanitize string) (sanitized string) {
	//replace whitespaces with _
	var noSpaces = whitespaceRegex.ReplaceAllString(toSanitize, "")
	//replace : with _
	var noColons = strings.Replace(noSpaces, "ga:", "", -1)
	noColons = strings.Replace(noColons, "rt:", "", -1)
	//strip all other non-alphanumeric except _
	var alphaNumUnderscore = alphanumericPlusUnderscoreRegex.FindAllString(noColons, -1)
	//return strings.ToLower(strings.Join(alphaNumUnderscore, ""))
	return strings.Join(alphaNumUnderscore, "")
}

//ASSUMPTION: Last element in header array is the metric name and all
//preceding elements are dimension names
func getMetric(gaData *analytics.RealtimeData) (metricName string) {
	lastHeaderIndex := len(gaData.ColumnHeaders) - 1
	lastHeader := gaData.ColumnHeaders[lastHeaderIndex]
	logp.Debug(logSelector, "metricName: %s", lastHeader.Name)
	return lastHeader.Name
}

func debugGAResponse(gaData *analytics.RealtimeData) {
	if logp.IsDebug(logSelector) {
		for i, columnHeader := range gaData.ColumnHeaders {
			logp.Debug(logSelector, "column header [%d]: %s %s %s ", i,
				columnHeader.ColumnType, columnHeader.DataType, columnHeader.Name)
		}
		for j, row := range gaData.Rows {
			for k, rowElement := range row {
				logp.Debug(logSelector, "data[%d][%d]", j, k, rowElement)
			}
		}
	}
}

func debugGAResponseForCharge(gaData *analytics.GaData) {
	logp.Debug(logSelector, "total Result : %v ", gaData.TotalsForAllResults)
	if logp.IsDebug(logSelector) {
		for i, columnHeader := range gaData.ColumnHeaders {
			logp.Debug(logSelector, "column header [%d]: %s %s %s ", i,
				columnHeader.ColumnType, columnHeader.DataType, columnHeader.Name)
		}
		for j, row := range gaData.Rows {
			for k, rowElement := range row {
				logp.Debug(logSelector, "data[%d][%d]", j, k, rowElement)
			}
		}
	}
}

func getGADataForCharge(gaIds, gaMetrics, gaDimensions, gaStarttime, gaEndtime string) (gaData *analytics.GaData, err error) {
	oauthHttpClient, err := google.DefaultClient(oauth2.NoContext, analytics.AnalyticsReadonlyScope)
	if err != nil {
		return nil, fmt.Errorf("Error creating auth context: %v", err)
	}
	analyticsService, err := analytics.New(oauthHttpClient)

	if err != nil {
		return nil, fmt.Errorf("Error creating Google Analytics client: %v", err)
	}

	dataGAService := analytics.NewDataGaService(analyticsService)

	re := regexp.MustCompile("(?P<month>[0-9]+)monthsAgo")
	spKey := re.SubexpNames()
	spVal := re.FindStringSubmatch(gaStarttime)
	mapVal := make(map[string]string)
	for i, value := range spVal {
		mapVal[spKey[i]] = value
	}
	var dataGAGetCall *analytics.DataGaGetCall
	if val, ok := mapVal["month"]; ok {
		if months, err := strconv.Atoi(val); err == nil {
			now := time.Now().AddDate(0, months*-1, 0)
			dataGAGetCall = dataGAService.Get(gaIds, now.Format("2006-01-02"), gaEndtime, gaMetrics)
		} else {
			dataGAGetCall = dataGAService.Get(gaIds, gaStarttime, gaEndtime, gaMetrics)
		}
	} else {
		dataGAGetCall = dataGAService.Get(gaIds, gaStarttime, gaEndtime, gaMetrics)
	}

	gaData, gaDataErr := dataGAGetCall.Dimensions(gaDimensions).Do()

	return gaData, gaDataErr
}

func parseGAResponseForCharge(gaData *analytics.GaData, dimensionCount, metricCount int) (GAData []GABeatDataPoint, err error) {
	debugGAResponseForCharge(gaData)
	if len(gaData.Rows) < 1 || len(gaData.Rows[0]) < 1 {
		return emptyResults, nil
	}
	gaDataPoints := []GABeatDataPoint{}

	for _, row := range gaData.Rows {
		data := make(map[string]interface{})
		for i, columnHeader := range gaData.ColumnHeaders {
			data[format(columnHeader.Name)] = row[i]
		}
		gaDataPoints = append(gaDataPoints, GABeatDataPoint{0, "", "", data})
	}
	/*
		for _, row := range gaData.Rows {
			var dimensionNames []string = row[0:dimensionCount]
			dimensionName := strings.Join(dimensionNames, "_")
			data := make(map[string]interface{})
			for idx := 0; idx < metricCount; idx++ {
				var metricValue string = row[dimensionCount+idx]
				var metricNameHeader string = gaData.ColumnHeaders[dimensionCount+idx].Name
				//dataPoint, err := strconv.Atoi(metricValue)
				dataPoint, err := strconv.ParseFloat(metricValue, 64)
				if err != nil {
					return errorResults, fmt.Errorf("Error converting string to int: %s, %v", metricValue, err)
				}
				gaDataPoint := GABeatDataPoint{int(dataPoint + 0.5),
					format(dimensionName),
					format(metricNameHeader),
					data}
				gaDataPoints = append(gaDataPoints, gaDataPoint)
			}
		}
	*/
	return gaDataPoints, nil
}
