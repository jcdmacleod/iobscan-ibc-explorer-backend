package monitor

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/model/entity"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/model/vo"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/monitor/metrics"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/repository"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/repository/cache"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/utils"
	"github.com/sirupsen/logrus"
)

var (
	cronTaskStatusMetric     metrics.Guage
	lcdConnectStatsMetric    metrics.Guage
	redisStatusMetric        metrics.Guage
	relayerStatusCheckMetric metrics.Guage
	TagName                  = "taskname"
	ChainTag                 = "chain_id"
	relayerTag               = "relayer_id"

	chainConfigRepo   repository.IChainConfigRepo   = new(repository.ChainConfigRepo)
	chainRegistryRepo repository.IChainRegistryRepo = new(repository.ChainRegistryRepo)
	relayerRepo       repository.IRelayerRepo       = new(repository.IbcRelayerRepo)
)

const (
	v1beta1        = "v1beta1"
	v1             = "v1"
	v1Channels     = "/ibc/core/channel/v1/channels?pagination.limit=1"
	apiChannels    = "/ibc/core/channel/%s/channels?pagination.offset=OFFSET&pagination.limit=LIMIT&pagination.count_total=true"
	apiClientState = "/ibc/core/channel/%s/channels/CHANNEL/ports/PORT/client_state"
)

// unbelievableLcd 不可信的lcd
var unbelievableLcd = map[string][]string{
	"sifchain_1": {"https://api.sifchain.chaintools.tech/"},
}

func NewMetricCronWorkStatus() metrics.Guage {
	syncWorkStatusMetric := metrics.NewGuage(
		"ibc_explorer_backend",
		"",
		"cron_task_status",
		"ibc_explorer_backend cron task working status (1:Normal  -1:UNormal)",
		[]string{TagName},
	)
	syncWorkStatus, _ := metrics.CovertGuage(syncWorkStatusMetric)
	return syncWorkStatus
}

func NewMetricRedisStatus() metrics.Guage {
	redisNodeStatusMetric := metrics.NewGuage(
		"ibc_explorer_backend",
		"redis",
		"connection_status",
		"ibc_explorer_backend  node connection status of redis service (1:Normal  -1:UNormal)",
		nil,
	)
	redisStatus, _ := metrics.CovertGuage(redisNodeStatusMetric)
	return redisStatus
}

func NewMetricLcdStatus() metrics.Guage {
	lcdConnectionStatusMetric := metrics.NewGuage(
		"ibc_explorer_backend",
		"lcd",
		"connection_status",
		"ibc_explorer_backend  lcd connection status of blockchain (1:Normal  -1:UNormal)",
		[]string{ChainTag},
	)
	connectionStatus, _ := metrics.CovertGuage(lcdConnectionStatusMetric)
	return connectionStatus
}

func NewMetricRelayerStatusCheck() metrics.Guage {
	lcdConnectionStatusMetric := metrics.NewGuage(
		"ibc_explorer_backend",
		"relayer",
		"status_check",
		"ibc_explorer_backend relayer status check result(1:Normal  -1:UNormal)",
		[]string{relayerTag},
	)
	connectionStatus, _ := metrics.CovertGuage(lcdConnectionStatusMetric)
	return connectionStatus
}

func SetCronTaskStatusMetricValue(taskName string, value float64) {
	if cronTaskStatusMetric != nil {
		cronTaskStatusMetric.With(TagName, taskName).Set(value)
	}
}

func lcdConnectionStatus(quit chan bool) {
	for {
		t := time.NewTimer(time.Duration(120) * time.Second)
		select {
		case <-t.C:
			chainCfgs, err := chainConfigRepo.FindAllOpenChainInfos()
			if err != nil {
				logrus.Error(err.Error())
				return
			}
			for _, val := range chainCfgs {
				if checkAndUpdateLcd(val.Lcd, val) {
					lcdConnectStatsMetric.With(ChainTag, val.ChainId).Set(float64(1))
				} else {
					if switchLcd(val) {
						lcdConnectStatsMetric.With(ChainTag, val.ChainId).Set(float64(1))
					} else {
						lcdConnectStatsMetric.With(ChainTag, val.ChainId).Set(float64(-1))
						logrus.Errorf("monitor chain %s lcd is unavailable", val.ChainId)
					}
				}
			}

		case <-quit:
			logrus.Debug("quit signal recv  lcdConnectionStatus")
			return

		}
	}
}

// checkAndUpdateLcd If lcd is ok, update db and return true. Else return false
func checkAndUpdateLcd(lcd string, cf *entity.ChainConfig) bool {
	unLcds, ex := unbelievableLcd[cf.ChainId]
	if ex && utils.InArray(unLcds, lcd) {
		return false
	}

	var ok bool
	var version string
	if _, err := utils.HttpGet(fmt.Sprintf("%s%s", lcd, v1Channels)); err == nil {
		ok = true
		version = v1
	} else if strings.Contains(err.Error(), "501 Not Implemented") {
		ok = true
		version = v1beta1
	} else {
		ok = false
	}

	if ok {
		if cf.Lcd == lcd && cf.LcdApiPath.ChannelsPath == fmt.Sprintf(apiChannels, version) && cf.LcdApiPath.ClientStatePath == fmt.Sprintf(apiClientState, version) {
			return true
		}

		cf.Lcd = lcd
		cf.LcdApiPath.ChannelsPath = fmt.Sprintf(apiChannels, version)
		cf.LcdApiPath.ClientStatePath = fmt.Sprintf(apiClientState, version)
		if err := chainConfigRepo.UpdateLcdApi(cf); err != nil {
			logrus.Error("lcd monitor update api error: %v", err)
			return false
		} else {
			return true
		}
	}

	return false
}

// switchLcd If Switch lcd succeeded, return true. Else return false
func switchLcd(chainConf *entity.ChainConfig) bool {
	chainRegistry, err := chainRegistryRepo.FindOne(chainConf.ChainId)
	if err != nil {
		logrus.Errorf("lcd monitor error: %v", err)
		return false
	}

	bz, err := utils.HttpGet(chainRegistry.ChainJsonUrl)
	if err != nil {
		logrus.Errorf("lcd monitor get chain json error: %v", err)
		return false
	}

	var chainRegisterResp vo.ChainRegisterResp
	_ = json.Unmarshal(bz, &chainRegisterResp)
	for _, v := range chainRegisterResp.Apis.Rest {
		if ok := checkAndUpdateLcd(v.Address, chainConf); ok {
			return true
		}
	}

	return false
}

func redisClientStatus(quit chan bool) {
	for {
		t := time.NewTimer(time.Duration(10) * time.Second)
		select {
		case <-t.C:
			if cache.RedisStatus() {
				redisStatusMetric.Set(float64(1))
			} else {
				redisStatusMetric.Set(float64(-1))
			}
		case <-quit:
			logrus.Debug("quit signal recv redisClientStatus")
			return
		}
	}
}

func relayerStatusCheck(quit chan bool) {
	for {
		t := time.NewTimer(time.Duration(600) * time.Second)
		select {
		case <-t.C:
			logrus.Info("monitor relayer start")
			relayerStatusCheckHandler()
		case <-quit:
			logrus.Debug("quit signal recv relayerStatusCheck")
			return
		}
	}
}

func relayerStatusCheckHandler() {
	var skip int64 = 0
	var limit int64 = 1000
	var timeOffset int64 = 1800
	for {
		relayerList, err := relayerRepo.FindAll(skip, limit)
		if err != nil {
			logrus.Errorf("monitor relayerStatusCheck relayerRepo.FindAll err, %v", err)
			return
		}

		for _, v := range relayerList {
			if v.Status == entity.RelayerRunning { // running
				if time.Now().Unix()-v.UpdateTime < v.TimePeriod+timeOffset {
					relayerStatusCheckMetric.With(relayerTag, v.RelayerId).Set(float64(1))
				} else {
					relayerStatusCheckMetric.With(relayerTag, v.RelayerId).Set(float64(-1))
					logrus.Warnf("monitor relayerStatusCheck relayer(%s) status(%d) may be incorrect", v.RelayerId, v.Status)
				}
			} else { // stop
				if time.Now().Unix()-v.UpdateTime > v.TimePeriod {
					relayerStatusCheckMetric.With(relayerTag, v.RelayerId).Set(float64(1))
				} else {
					relayerStatusCheckMetric.With(relayerTag, v.RelayerId).Set(float64(-1))
					logrus.Warnf("monitor relayerStatusCheck relayer(%s) status(%d) may be incorrect", v.RelayerId, v.Status)
				}
			}
		}

		if len(relayerList) < int(limit) {
			break
		}
		skip += limit
	}
}

func Start(port string) {
	quit := make(chan bool)
	defer func() {
		close(quit)
		if err := recover(); err != nil {
			logrus.Error("monitor server occur error ", err)
			os.Exit(1)
		}
	}()
	logrus.Info("monitor server start")
	// start monitor
	server := metrics.NewMonitor(port)
	cronTaskStatusMetric = NewMetricCronWorkStatus()
	redisStatusMetric = NewMetricRedisStatus()
	lcdConnectStatsMetric = NewMetricLcdStatus()
	relayerStatusCheckMetric = NewMetricRelayerStatusCheck()
	server.Report(func() {
		go redisClientStatus(quit)
		go lcdConnectionStatus(quit)
		go relayerStatusCheck(quit)
	})
}
