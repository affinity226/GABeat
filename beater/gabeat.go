package beater

import (
	"fmt"
	"github.com/robfig/cron"
	"time"

	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"
	"github.com/elastic/beats/libbeat/publisher"

	"github.com/affinity226/gabeat/config"
	"github.com/affinity226/gabeat/ga"
)

var debugf = logp.MakeDebug("gabeat")

type gaDataRetriever func(gaConfig config.GoogleAnalyticsConfig) (gaDataPoints []ga.GABeatDataPoint, err error)

type Gabeat struct {
	done   chan struct{}
	config config.Config
	client publisher.Client
}

// Creates beater
func New(b *beat.Beat, cfg *common.Config) (beat.Beater, error) {
	config := config.DefaultConfig
	if err := cfg.Unpack(&config); err != nil {
		return nil, fmt.Errorf("Error reading config file: %v", err)
	}

	bt := &Gabeat{
		done:   make(chan struct{}),
		config: config,
	}
	logp.Info("Config: %s", bt.config)
	return bt, nil
}

func (bt *Gabeat) Run(b *beat.Beat) error {
	return runFunctionally(bt, b, ga.GetGAReportData)
}

/*
func runFunctionally(bt *Gabeat, b *beat.Beat, dataFunc gaDataRetriever) error {
	logp.Info("gabeat is running! Hit CTRL-C to stop it.")
	bt.client = b.Publisher.Connect()
	ticker := time.NewTicker(bt.config.Period)
	for {
		select {
		case <-bt.done:
			return nil
		case <-ticker.C:
		} //end select
		for _, gaConfig := range bt.config.Googleanalytics {
			beatOnce(bt.client, b.Name, gaConfig, dataFunc) //Charge
		}
	} //end for
} //end func
*/
func runFunctionally(bt *Gabeat, b *beat.Beat, dataFunc gaDataRetriever) error {
	logp.Info("gabeat is running! Hit CTRL-C to stop it.")
	bt.client = b.Publisher.Connect()
	cron := cron.New()
	for idx, _ := range bt.config.Googleanalytics {
		gaConfig := bt.config.Googleanalytics[idx]
		cron.AddFunc(gaConfig.Schedule, func() { beatOnce(bt.client, b.Name, gaConfig, dataFunc) }) //Charge
	}
	cron.Start()
	for {
		select {
		case <-bt.done:
			return nil
		} //end select
	} //end for
} //end func

func beatOnce(client publisher.Client, beatName string, gaConfig config.GoogleAnalyticsConfig, dataFunc gaDataRetriever) {
	GAData, err := dataFunc(gaConfig)
	if err == nil {
		//publishToElastic(client, beatName, GAData)
		publishToElasticForCharge(client, gaConfig, GAData)
	} else {
		logp.Err("gadata was null, not publishing: %v", err)
	}

}

func makeEvent(beatType string, GAData []ga.GABeatDataPoint) map[string]interface{} {
	event := common.MapStr{
		"@timestamp": common.Time(time.Now()),
		"type":       beatType,
		"count":      1, //The number of transactions that this event represents
	}
	for _, gaDataPoint := range GAData {
		gaDataName := gaDataPoint.DimensionName + "_" + gaDataPoint.MetricName
		event.Put(gaDataName, gaDataPoint.Value)
	}
	return event
}

func publishToElastic(client publisher.Client, beatType string, GAData []ga.GABeatDataPoint) {
	event := makeEvent(beatType, GAData)
	succeeded := client.PublishEvent(event)
	if !succeeded {
		logp.Err("Publisher couldn't publish event to Elastic")
	}
}

func (bt *Gabeat) Stop() {
	bt.client.Close()
	close(bt.done)
}

func publishToElasticForCharge(client publisher.Client, gaConfig config.GoogleAnalyticsConfig, GAData []ga.GABeatDataPoint) {
	for _, data := range GAData {
		event := makeEventForCharge(gaConfig, data)
		succeeded := client.PublishEvent(event)
		if !succeeded {
			logp.Err("Publisher couldn't publish event to Elastic")
		}
	}
}

//func makeEventForCharge(beatType string, GAData []ga.GABeatDataPoint) map[string]interface{} {
func makeEventForCharge(gaConfig config.GoogleAnalyticsConfig, GAData ga.GABeatDataPoint) map[string]interface{} {
	event := common.MapStr{
		"@timestamp": common.Time(time.Now()),
		"type":       gaConfig.DocumentType,
		//"count":      1, //The number of transactions that this event represents
		"tags": gaConfig.Tags,
	}
	for key, value := range GAData.Data {
		if key == "date" {
			logxTimestamp, err := time.Parse("20060102", value.(string))
			if err == nil {
				event.Put("logx-timestamp", logxTimestamp)
			} else {
				event.Put("logx-timestamp", value)
			}
		} else {
			event.Put(key, value)
		}
	}
	return event
}
