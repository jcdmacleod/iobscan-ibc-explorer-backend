package task

import (
	"fmt"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/global"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/model/dto"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/model/entity"
	"github.com/qiniu/qmgo"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
	"strings"
	"time"
)

type (
	RelayerStatisticsTask struct {
		distRelayerMap map[string]bool
	}
	Statistic struct {
		*entity.IBCRelayer
		Amounts    decimal.Decimal
		Txs        int64
		TxsSuccess int64
		BaseDenom  string
	}
)

func (t *RelayerStatisticsTask) Name() string {
	return "ibc_relayer_statistics_task"
}

func (t *RelayerStatisticsTask) relayerTxsMapKey(statisticId, address, baseDenom string) string {
	return fmt.Sprintf("%s:%s:%s", statisticId, address, baseDenom)
}

//only run once for relayer data
func (t *RelayerStatisticsTask) NewOnlyInitRelayerOnce() int {
	startTime := time.Now().Unix()
	historySegments, err := getHistorySegment()
	if err != nil {
		logrus.Errorf("task %s getHistorySegment err, %v", t.Name(), err)
		return -1
	}
	//insert relayer data
	t.handleNewRelayerOnce(historySegments, true)

	segments, err := getSegment()
	if err != nil {
		logrus.Errorf("task %s getSegment err, %v", t.Name(), err)
		return -1
	}
	//insert relayer data
	t.handleNewRelayerOnce(segments, false)
	logrus.Infof("task %s finish deal, time use %d(s)", t.Name(), time.Now().Unix()-startTime)
	return 1
}

func (t *RelayerStatisticsTask) Switch() bool {
	return global.Config.Task.SwitchIbcRelayerStatisticsTask
}

func (t *RelayerStatisticsTask) Run() int {
	if t.Switch() {
		ret := t.NewOnlyInitRelayerOnce()
		if ret > 0 {
			logrus.Infof("task only init relayer data %s ok", t.Name())
		}
		return ret
	}
	historySegments, err := getHistorySegment()
	if err != nil {
		logrus.Errorf("task %s getHistorySegment err, %v", t.Name(), err)
		return -1
	}
	logrus.Infof("task %s deal history segment total: %d", t.Name(), len(historySegments))
	startTime := time.Now().Unix()
	//insert relayer data
	t.handleNewRelayerOnce(historySegments, true)

	if err = t.dealHistory(historySegments); err != nil {
		logrus.Errorf("task %s dealHistory err, %v", t.Name(), err)
		return -1
	}

	logrus.Infof("task %s finish dealHistory, time use %d(s)", t.Name(), time.Now().Unix()-startTime)
	segments, err := getSegment()
	if err != nil {
		logrus.Errorf("task %s getSegment err, %v", t.Name(), err)
		return -1
	}
	startTime = time.Now().Unix()
	logrus.Infof("task %s deal segment total: %d", t.Name(), len(segments))
	//insert relayer data
	t.handleNewRelayerOnce(segments, false)

	logrus.Infof("task %s deal segment total: %d", t.Name(), len(segments))
	if err = t.deal(segments, opInsert); err != nil {
		logrus.Errorf("task %s deal err, %v", t.Name(), err)
		return -1
	}
	logrus.Infof("task %s finish deal, time use %d(s)", t.Name(), time.Now().Unix()-startTime)

	return 1
}

func (t *RelayerStatisticsTask) saveData(relayerStaticsMap map[string]Statistic, startTime, endTime int64, op int) error {
	var relayerStatics []entity.IBCRelayerStatistics
	for key, value := range relayerStaticsMap {
		if arrs := strings.Split(key, ":"); len(arrs) == 4 {
			statisticId, address, baseDenom, baseDenomChainId := arrs[0], arrs[1], arrs[2], arrs[3]
			item := createIBCRelayerStatistics(address, baseDenom, baseDenomChainId, value.Amounts,
				value.TxsSuccess, value.Txs, startTime, endTime)
			item.StatisticId = statisticId
			relayerStatics = append(relayerStatics, item)
		}
	}
	if len(relayerStatics) > 0 {
		switch op {
		case opInsert:
			if err := relayerStatisticsRepo.Insert(relayerStatics); err != nil && !qmgo.IsDup(err) {
				return err
			}
		case opUpdate:
			for _, val := range relayerStatics {
				if err := relayerStatisticsRepo.InserOrUpdate(val); err != nil && err != qmgo.ErrNoSuchDocuments {
					logrus.Error("relayer statistic update fail, ", err.Error())
				}
			}

		}

	}
	return nil
}

// dealHistory 处理历史记录，针对ex_ibc_tx
func (t *RelayerStatisticsTask) dealHistory(segments []*segment) error {
	for _, v := range segments {
		relayerSuccessTxs, err := ibcTxRepo.CountHistoryRelayerSuccessPacketTxs(v.StartTime, v.EndTime)
		if err != nil {
			logrus.Error("Count History RelayerSuccessPacketTxs  have fail, ", err.Error())
			continue
		}
		relayerAmounts, err := ibcTxRepo.CountHistoryRelayerPacketAmount(v.StartTime, v.EndTime)
		if err != nil {
			logrus.Error("Count History RelayerPacketAmount  have fail, ", err.Error())
			continue
		}
		aggr := t.aggr(relayerSuccessTxs, relayerAmounts)
		if err = t.saveData(aggr, v.StartTime, v.EndTime, opInsert); err != nil {
			return err
		}
		logrus.Debugf("dealHistory task %s scan ex_ibc_tx finish segment [%v:%v]", t.Name(), v.StartTime, v.EndTime)
	}
	return nil
}

// deal 处理最新的记录，针对ex_ibc_tx_latest
func (t *RelayerStatisticsTask) deal(segments []*segment, op int) error {
	for _, v := range segments {
		relayerSuccessTxs, err := ibcTxRepo.CountRelayerSuccessPacketTxs(v.StartTime, v.EndTime)
		if err != nil {
			logrus.Error("collectTx  have fail, ", err.Error())
			continue
		}
		relayerAmounts, err := ibcTxRepo.CountRelayerPacketTxsAndAmount(v.StartTime, v.EndTime)
		if err != nil {
			logrus.Error(err.Error())
		}
		aggr := t.aggr(relayerSuccessTxs, relayerAmounts)
		if err := t.saveData(aggr, v.StartTime, v.EndTime, op); err != nil {
			return err
		}
		logrus.Debugf("deal task %s scan ex_ibc_tx_latest finish segment [%v:%v]", t.Name(), v.StartTime, v.EndTime)
	}
	return nil
}

func (t *RelayerStatisticsTask) aggr(relayerSuccessTxs []*dto.CountRelayerPacketTxsCntDTO, relayerAmounts []*dto.CountRelayerPacketAmountDTO) map[string]Statistic {
	relayerTxsMap := make(map[string]TxsItem, 20)

	for _, tx := range relayerSuccessTxs {
		if !tx.Valid() {
			continue
		}
		statisticId, _ := relayerStatisticsRepo.CreateStatisticId(tx.ScChainId, tx.DcChainId, tx.ScChannel, tx.DcChannel)
		key := t.relayerTxsMapKey(statisticId, tx.DcChainAddress, tx.BaseDenom)
		relayerTxsMap[key] = TxsItem{
			TxsSuccess: tx.Count,
		}
	}

	relayerAmtsMap := createAmounts(relayerAmounts)
	relayerStaticsMap := make(map[string]Statistic, 20)
	for key, val := range relayerAmtsMap {
		if _, exist := relayerStaticsMap[key]; exist {
			continue
		}
		arrs := strings.Split(key, ":")
		statisticId, relayerAddr, denom := arrs[0], arrs[1], arrs[2]
		datas := strings.Split(statisticId, "|")
		srcInfo := strings.Join([]string{datas[0], datas[1]}, "|")
		dscInfo := strings.Join([]string{datas[2], datas[3]}, "|")

		key1 := strings.Join([]string{srcInfo, dscInfo}, "|")
		key2 := strings.Join([]string{dscInfo, srcInfo}, "|")
		var one Statistic
		for key, value := range relayerTxsMap {
			if strings.Contains(key, relayerAddr) && strings.Contains(key, denom) && (strings.Contains(key, key1) || strings.Contains(key, key2)) {
				one.TxsSuccess = value.TxsSuccess
			}
		}
		one.Amounts = val.Amt
		one.Txs = val.Txs
		relayerStaticsMap[key] = one
	}

	return relayerStaticsMap
}
func (t *RelayerStatisticsTask) initdistRelayerMap() {
	t.distRelayerMap = make(map[string]bool, 20)
	skip := int64(0)
	limit := int64(50)
	for {
		dbRelayers, err := relayerRepo.FindAll(skip, limit)
		if err != nil {
			logrus.Error("find relayer by page fail, ", err.Error())
			return
		}

		for _, val := range dbRelayers {
			key := fmt.Sprintf("%s:%s:%s", val.ChainA, val.ChainAAddress, val.ChannelA)
			key1 := fmt.Sprintf("%s:%s:%s", val.ChainB, val.ChainBAddress, val.ChannelB)
			t.distRelayerMap[key] = true
			t.distRelayerMap[key1] = true
		}
		if len(dbRelayers) < int(limit) {
			break
		}
		skip += limit
	}

	return
}

func (t *RelayerStatisticsTask) handleNewRelayerOnce(segments []*segment, historyData bool) {
	t.initdistRelayerMap()
	for _, v := range segments {
		var relayersData []entity.IBCRelayer
		if historyData {
			relayersData = handleIbcTxHistory(v.StartTime, v.EndTime)
		} else {
			relayersData = handleIbcTxLatest(v.StartTime, v.EndTime)
		}
		if len(relayersData) > 0 {
			relayersData = distinctRelayer(relayersData, t.distRelayerMap)
			relayersData = filterDbExist(relayersData, t.distRelayerMap)
			if len(relayersData) == 0 {
				continue
			}
			if err := relayerRepo.Insert(relayersData); err != nil && !qmgo.IsDup(err) {
				logrus.Error("insert  relayer data fail, ", err.Error())
			}
		}
		logrus.Debugf("task %s find relayer finish segment [%v:%v], relayers:%v", t.Name(), v.StartTime, v.EndTime, len(relayersData))
	}
}

func handleIbcTxHistory(startTime, endTime int64) []entity.IBCRelayer {
	relayerDtos, err := ibcTxRepo.GetHistoryRelayerInfo(startTime, endTime)
	if err != nil {
		logrus.Errorf("get relayer info fail, %s", err.Error())
		return nil
	}
	var relayers []entity.IBCRelayer
	for _, val := range relayerDtos {
		relayers = append(relayers, createRelayerData(val))
	}
	return relayers
}