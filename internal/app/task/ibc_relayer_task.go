package task

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/constant"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/model/dto"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/model/entity"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/model/vo"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/task/fsmtool"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/utils"
	"github.com/qiniu/qmgo"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/mongo"
)

type IbcRelayerCronTask struct {
	chainConfigMap map[string]*entity.ChainConfig
	//key:address+Chain+Channel
	relayerTxsDataMap map[string]TxsItem
	//key:address+Chain+Channel
	relayerValueMap  map[string]decimal.Decimal
	relayerTxsAmtMap map[string]TxsAmtItem
	//key: ChainA+ChainB+ChannelA+ChannelB
	channelRelayerCnt map[string]int64
	//key: BaseDenom+ChainId
	denomPriceMap        map[string]CoinItem
	channelUpdateTimeMap *sync.Map
}
type (
	TxsItem struct {
		Txs        int64
		TxsSuccess int64
	}
	TxsAmtItem struct {
		Txs int64
		Amt decimal.Decimal
	}

	CoinItem struct {
		Price float64
		Scale int
	}
	Info struct {
		TimePeriod int64
		UpdateTime int64
	}
)

type RelayerHandle func(relayer *entity.IBCRelayer)

func relayerAmtsMapKey(statisticId, baseDenom, dcChainAddr, baseDenomChainId string) string {
	return fmt.Sprintf("%s:%s:%s:%s", statisticId, dcChainAddr, baseDenom, baseDenomChainId)
}

func relayerAmtValueMapKey(statisticId, address string) string {
	return fmt.Sprintf("%s:%s", statisticId, address)
}

func (t *IbcRelayerCronTask) relayerTxsDataMapKey(statisticId, dcChainAddr string) string {
	return fmt.Sprintf("%s:%s", statisticId, dcChainAddr)
}

func (t *IbcRelayerCronTask) Name() string {
	return "ibc_relayer_task"
}
func (t *IbcRelayerCronTask) Cron() int {
	if taskConf.CronTimeRelayerTask > 0 {
		return taskConf.CronTimeRelayerTask
	}
	return ThreeMinute
}

func (t *IbcRelayerCronTask) Run() int {
	if err := t.init(); err != nil {
		return -1
	}

	t.getTokenPriceMap()
	_ = t.todayStatistics()
	_ = t.yesterdayStatistics()
	t.cacheChainUnbondTimeFromLcd()
	t.cacheIbcChannelRelayer()

	t.caculateRelayerTotalValue()
	t.AggrRelayerPacketTxs()
	t.CheckAndChangeRelayer()
	//最后更新chains
	t.updateIbcChainsRelayer()

	return 1
}

func (t *IbcRelayerCronTask) init() error {
	if chainConfigMap, err := getAllChainMap(); err != nil {
		logrus.Errorf("task %s getAllChainMap err, %v", t.Name(), err)
		return err
	} else {
		t.chainConfigMap = chainConfigMap
	}

	t.channelUpdateTimeMap = new(sync.Map)
	return nil
}

//use cache map check relayer if exist
func filterDbExist(relayers []entity.IBCRelayer, distRelayerMap map[string]bool) []entity.IBCRelayer {
	var distinctArr []entity.IBCRelayer
	for _, val := range relayers {
		key := fmt.Sprintf("%s:%s:%s", val.ChainA, val.ChainAAddress, val.ChannelA)
		key1 := fmt.Sprintf("%s:%s:%s", val.ChainB, val.ChainBAddress, val.ChannelB)
		_, exist := distRelayerMap[key]
		_, exist1 := distRelayerMap[key1]
		if !exist && !exist1 && val.Valid() {
			distRelayerMap[key] = true
			distRelayerMap[key1] = true
			val.RelayerId = utils.Md5(val.ChannelA + val.ChannelB + val.ChainA + val.ChainB + val.ChainAAddress + val.ChainBAddress)
			distinctArr = append(distinctArr, val)
		}
	}
	return distinctArr
}
func getSrcChainAddress(info *dto.GetRelayerInfoDTO, historyData bool) []string {
	//查询relayer在原链所有地址
	var (
		chainAAddress []string
		msgPacketId   string
	)

	if historyData {
		ibcTx, err := ibcTxRepo.GetHistoryOneRelayerScTxPacketId(info)
		if err == nil {
			msgPacketId = ibcTx.ScTxInfo.Msg.CommonMsg().PacketId
		}
	} else {
		ibcTx, err := ibcTxRepo.GetOneRelayerScTxPacketId(info)
		if err == nil {
			msgPacketId = ibcTx.ScTxInfo.Msg.CommonMsg().PacketId
		}
	}
	if msgPacketId != "" {
		scAddr, err := txRepo.GetRelayerScChainAddr(msgPacketId, info.ScChainId)
		if err != nil && err != qmgo.ErrNoSuchDocuments {
			logrus.Errorf("get srAddr relayer packetId fail, %s", err.Error())
		}
		if scAddr != "" {
			chainAAddress = append(chainAAddress, scAddr)
		}
	}
	return chainAAddress
}

func distinctRelayer(relayers []entity.IBCRelayer, distRelayerMap map[string]bool) []entity.IBCRelayer {
	var distinctArr []entity.IBCRelayer
	checkSameMap := make(map[string]string, 20)
	//收集relayer双向链的地址
	for _, val := range relayers {
		if val.ChainBAddress != "" {
			keyB := fmt.Sprintf("%s%s", val.ChainB, val.ChannelB)
			checkSameMap[keyB] = val.ChainBAddress
		} else if val.ChainAAddress != "" {
			keyA := fmt.Sprintf("%s%s", val.ChainA, val.ChannelA)
			checkSameMap[keyA] = val.ChainAAddress
		}
	}
	for _, val := range relayers {
		//获取对方链地址信息
		if val.ChainBAddress == "" {
			key := fmt.Sprintf("%s%s", val.ChainB, val.ChannelB)
			val.ChainBAddress, _ = checkSameMap[key]
		}
		//获取对方链地址信息
		if val.ChainAAddress == "" {
			key := fmt.Sprintf("%s%s", val.ChainA, val.ChannelA)
			val.ChainAAddress, _ = checkSameMap[key]
		}
		key := fmt.Sprintf("%s:%s:%s", val.ChainA, val.ChannelA, val.ChainAAddress)
		key1 := fmt.Sprintf("%s:%s:%s", val.ChainB, val.ChannelB, val.ChainBAddress)
		_, exist := distRelayerMap[key]
		_, exist1 := distRelayerMap[key1]
		if !exist && !exist1 {
			distRelayerMap[key] = true
			distRelayerMap[key1] = true
			distinctArr = append(distinctArr, val)
		}
	}
	return distinctArr
}

//dependence: cacheChainUnbondTimeFromLcd
func (t *IbcRelayerCronTask) updateRelayerStatus(relayer *entity.IBCRelayer) {
	var value Info
	var channelMatchRet int
	//get latest update_client time
	value.TimePeriod, value.UpdateTime, channelMatchRet = t.getTimePeriodAndupdateTime(relayer)
	if value.TimePeriod == -1 {
		if relayer.TimePeriod <= 0 {
			//get unbonding time from cache
			var chainAUnbondT, chainBUnbondT int64
			chainAUnbondTime, _ := unbondTimeCache.GetUnbondTime(relayer.ChainA)
			if chainAUnbondTime != "" {
				chainAUnbondT, _ = strconv.ParseInt(strings.ReplaceAll(chainAUnbondTime, "s", ""), 10, 64)
			}
			chainBUnbondTime, _ := unbondTimeCache.GetUnbondTime(relayer.ChainB)
			if chainBUnbondTime != "" {
				chainBUnbondT, _ = strconv.ParseInt(strings.ReplaceAll(chainBUnbondTime, "s", ""), 10, 64)
			}
			if chainAUnbondT > 0 && chainBUnbondT > 0 {
				if chainAUnbondT >= chainBUnbondT {
					value.TimePeriod = chainBUnbondT
				} else {
					value.TimePeriod = chainAUnbondT
				}
			} else { // 有一个unbound time查不到
				bondT := chainAUnbondT + chainBUnbondT
				if bondT > 0 {
					value.TimePeriod = bondT
				} else {
					value.TimePeriod = constant.DefaultUnboundTime
				}
			}
		} else {
			value.TimePeriod = relayer.TimePeriod
		}
	}

	t.handleOneRelayerStatusAndTime(relayer, value.UpdateTime, value.TimePeriod, channelMatchRet)
	if channelMatchRet != channelMatchSuccess {
		//如果update_client不是该relayer对应的通道，不更新channel页channel更新时间
		value.UpdateTime = 0
	}
	t.updateIbcChannelRelayerInfo(relayer, value.UpdateTime)
}
func (t *IbcRelayerCronTask) CheckAndChangeRelayer() {
	skip := int64(0)
	limit := int64(1000)
	threadNum := 10
	for {
		relayers, err := relayerRepo.FindAll(skip, limit)
		if err != nil {
			logrus.Error("find relayer by page fail, ", err.Error())
			return
		}
		startTime := time.Now().Unix()
		chanLimit := make(chan bool, threadNum)
		for _, relayer := range relayers {
			chanLimit <- true
			go func(relayer *entity.IBCRelayer, chanLimit chan bool) {
				defer func() {
					if r := recover(); r != nil {
						logrus.Error("recover serious error in updateRelayerStatus or saveOrUpdateRelayerTxsAndValue ", r)
					}
					<-chanLimit
				}()
				t.updateRelayerStatus(relayer)
				t.saveOrUpdateRelayerTxsAndValue(relayer)

			}(relayer, chanLimit)
		}
		logrus.Debugf("checkAndChangeRelayer page(relayers: %d) end, time use %d(s)d", len(relayers), time.Now().Unix()-startTime)
		if len(relayers) < int(limit) {
			break
		}
		skip += limit
	}
}

func (t *IbcRelayerCronTask) getTokenPriceMap() {
	coinIdPriceMap, _ := tokenPriceRepo.GetAll()
	baseDenoms, err := baseDenomCache.FindAll()
	if err != nil {
		logrus.Error("find base_denom fail, ", err.Error())
		return
	}
	if len(coinIdPriceMap) == 0 {
		return
	}
	t.denomPriceMap = make(map[string]CoinItem, len(baseDenoms))
	for _, val := range baseDenoms {
		if price, ok := coinIdPriceMap[val.CoinId]; ok {
			t.denomPriceMap[val.Denom+val.ChainId] = CoinItem{Price: price, Scale: val.Scale}
		}
	}
}

func (t *IbcRelayerCronTask) cacheChainUnbondTimeFromLcd() {
	configList, err := chainConfigRepo.FindAll()
	if err != nil {
		logrus.Errorf("task %s cacheChainUnbondTimeFromLcd error, %v", t.Name(), err)
		return
	}
	if len(configList) == 0 {
		return
	}
	value, _ := unbondTimeCache.GetUnbondTime(configList[0].ChainId)
	if len(value) > 0 {
		return
	}
	group := sync.WaitGroup{}
	group.Add(len(configList))
	for _, val := range configList {
		baseUrl := strings.ReplaceAll(fmt.Sprintf("%s%s", val.Lcd, val.LcdApiPath.ParamsPath), entity.ParamsModulePathPlaceholder, entity.StakeModule)
		go func(baseUrl, chainId string) {
			getStakeParams(baseUrl, chainId)
			group.Done()
		}(baseUrl, val.ChainId)
	}
	group.Wait()
}

func getStakeParams(baseUrl, chainId string) {
	bz, err := utils.HttpGet(baseUrl)
	if err != nil {
		logrus.Errorf(" staking %s params error, %v", baseUrl, err)
		return
	}

	var stakeparams vo.StakeParams
	err = json.Unmarshal(bz, &stakeparams)
	if err != nil {
		logrus.Errorf("unmarshal staking params error, %v", err)
		return
	}
	_ = unbondTimeCache.SetUnbondTime(chainId, stakeparams.Params.UnbondingTime)
}

func getConnectionFromLcd(baseUrl string) []string {
	bz, err := utils.HttpGet(baseUrl)
	if err != nil {
		logrus.Error(err)
		return nil
	}

	var connections vo.Connections
	err = json.Unmarshal(bz, &connections)
	if err != nil {
		logrus.Error(err)
		return nil
	}
	return connections.ConnectionPaths
}

func getChannelFromLcd(baseUrl string) []vo.LcdChannel {
	bz, err := utils.HttpGet(baseUrl)
	if err != nil {
		logrus.Error(err)
		return nil
	}

	var connections vo.ConnectionChannels
	err = json.Unmarshal(bz, &connections)
	if err != nil {
		logrus.Error(err)
		return nil
	}
	return connections.Channels
}

func (t *IbcRelayerCronTask) handleToUnknow(relayer *entity.IBCRelayer, paths []*entity.ChannelPath, updateTime, timePeriod int64) {
	f := fsmtool.NewIbcRelayerFSM(entity.RelayerRunningStr)
	//Running=>Close: update_client时间与当前时间差大于relayer基准周期
	//处理新基准时间波动情况误差20秒
	if timePeriod > 0 && timePeriod+20 < time.Now().Unix()-updateTime {
		relayer.Status = entity.RelayerStop
		if err := f.Event(fsmtool.IbcRelayerEventUnknown, relayer); err == nil {
			f.SetState(entity.RelayerRunningStr)
		} else {
			logrus.Warn("machine status event to running->unknown failed, " + err.Error())
		}
		return
	}
	//Running=>Close: relayer中继通道只要出现状态不是STATE_OPEN
	for _, path := range paths {
		if path.ChannelId == relayer.ChannelA && path.Counterparty.ChannelId == relayer.ChannelB {
			if path.State != constant.ChannelStateOpen || path.Counterparty.State != constant.ChannelStateOpen {
				relayer.Status = entity.RelayerStop
				if err := f.Event(fsmtool.IbcRelayerEventUnknown, relayer); err == nil {
					f.SetState(entity.RelayerRunningStr)
					break
				} else {
					logrus.Warn("machine status event to running->unknown failed, " + err.Error())
				}
			}
		}
	}
}

// Close=>Running: relayer的双向通道状态均为STATE_OPEN且update_client 时间与当前时间差小于relayer基准周期
func (t *IbcRelayerCronTask) handleToRunning(relayer *entity.IBCRelayer, paths []*entity.ChannelPath, updateTime, timePeriod int64) {
	f := fsmtool.NewIbcRelayerFSM(entity.RelayerStopStr)
	if updateTime > 0 && timePeriod > 0 && timePeriod > time.Now().Unix()-updateTime {
		var channelOpen bool
		if len(paths) == 0 {
			return
		}
		for _, path := range paths {
			if path.ChannelId == relayer.ChannelA && path.Counterparty.ChannelId == relayer.ChannelB &&
				path.State == constant.ChannelStateOpen && path.Counterparty.State == constant.ChannelStateOpen {
				channelOpen = true
				break
			}
		}
		if channelOpen {
			relayer.Status = entity.RelayerRunning
			if err := f.Event(fsmtool.IbcRelayerEventRunning, relayer); err == nil {
				f.SetState(entity.RelayerStopStr)
			} else {
				logrus.Error("machine status event to unknown->running failed, " + err.Error())
			}
		}
	} else if relayer.TimePeriod == -1 && relayer.UpdateTime == 0 && updateTime > 0 { //新relayer
		relayer.Status = entity.RelayerRunning
		if err := f.Event(fsmtool.IbcRelayerEventRunning, relayer); err == nil {
			f.SetState(entity.RelayerStopStr)
		} else {
			logrus.Error("machine status event to unknown->running failed, " + err.Error())
		}
	}
}
func (t *IbcRelayerCronTask) handleOneRelayerStatusAndTime(relayer *entity.IBCRelayer, updateTime, timePeriod int64, mathChannelRet int) {
	paths := t.getChannelsStatus(relayer.ChainA, relayer.ChainB)
	//Running=>Close: relayer中继通道只要出现状态不是STATE_OPEN
	if relayer.Status == entity.RelayerRunning {
		t.handleToUnknow(relayer, paths, updateTime, timePeriod)
	} else {
		// Close=>Running: relayer的双向通道状态均为STATE_OPEN且update_client 时间与当前时间差小于relayer基准周期
		t.handleToRunning(relayer, paths, updateTime, timePeriod)
	}
	if err := relayerRepo.UpdateStatusAndTime(relayer.RelayerId, 0, updateTime, timePeriod); err != nil {
		logrus.Error("update relayer about time_period and update_time fail, ", err.Error())
	}
}

// dependence: cacheIbcChannelRelayer
func (t *IbcRelayerCronTask) updateIbcChannelRelayerInfo(relayer *entity.IBCRelayer, updateTime int64) {
	if len(t.channelRelayerCnt) > 0 || updateTime > 0 {
		var relayerCnt int64
		if len(t.channelRelayerCnt) > 0 {
			if relayercnt, ok := t.channelRelayerCnt[relayer.ChainA+relayer.ChainB+relayer.ChannelA+relayer.ChannelB]; ok {
				relayerCnt += relayercnt
			}
			if relayercnt, ok := t.channelRelayerCnt[relayer.ChainB+relayer.ChainA+relayer.ChannelB+relayer.ChannelA]; ok {
				relayerCnt += relayercnt
			}
		}

		ChannelId := generateChannelId(relayer.ChainA, relayer.ChannelA, relayer.ChainB, relayer.ChannelB)
		value, ok := t.channelUpdateTimeMap.Load(ChannelId)
		if ok && value.(int64) > updateTime {
			updateTime = 0
		} else {
			t.channelUpdateTimeMap.Store(ChannelId, updateTime)
		}

		if err := channelRepo.UpdateOne(ChannelId, updateTime, relayerCnt); err != nil && err != mongo.ErrNoDocuments {
			logrus.Error("update ibc_channel about relayer fail, ", err.Error())
		}
	}
}

//set cache value redis key: ibc_channel_relayer_cnt
func (t *IbcRelayerCronTask) cacheIbcChannelRelayer() {
	channelRelayersDto, err := relayerRepo.CountChannelRelayers()
	if err != nil {
		logrus.Error("count channel relayers fail, ", err.Error())
		return
	}
	t.channelRelayerCnt = make(map[string]int64, len(channelRelayersDto))
	for _, one := range channelRelayersDto {
		t.channelRelayerCnt[one.ChainA+one.ChainB+one.ChannelA+one.ChannelB] = one.Count
	}
}
func collectTxs(data []*dto.CountRelayerPacketTxsCntDTO, startTime, endTime int64, hookTxs func(startTime, endTime int64) ([]*dto.CountRelayerPacketTxsCntDTO,
	error)) []*dto.CountRelayerPacketTxsCntDTO {

	relayerTxsCntDto, err := hookTxs(startTime, endTime)
	if err != nil {
		logrus.Error("collectTx hookTxs have fail, ", err.Error())
		return data
	}

	data = append(data, relayerTxsCntDto...)

	return data
}

func (t *IbcRelayerCronTask) AggrRelayerPacketTxs() {
	res, err := relayerStatisticsRepo.AggregateRelayerTxs()
	if err != nil {
		logrus.Error("aggregate relayer txs have fail, ", err.Error())
		return
	}
	t.relayerTxsDataMap = make(map[string]TxsItem, 20)
	for _, item := range res {
		key := t.relayerTxsDataMapKey(item.StatisticId, item.Address)
		value, exist := t.relayerTxsDataMap[key]
		if exist {
			value.Txs += item.TotalTxs
			value.TxsSuccess += item.SuccessTotalTxs
			t.relayerTxsDataMap[key] = value
		} else {
			t.relayerTxsDataMap[key] = TxsItem{Txs: item.TotalTxs, TxsSuccess: item.SuccessTotalTxs}
		}
	}
}

func createAmounts(relayerAmounts []*dto.CountRelayerPacketAmountDTO) map[string]TxsAmtItem {
	relayerAmtsMap := make(map[string]TxsAmtItem, 20)
	for _, amt := range relayerAmounts {
		if !amt.Valid() {
			continue
		}
		statisticId, _ := relayerStatisticsRepo.CreateStatisticId(amt.ScChainId, amt.DcChainId, amt.ScChannel, amt.DcChannel)
		key := relayerAmtsMapKey(statisticId, amt.BaseDenom, amt.DcChainAddress, amt.BaseDenomChainId)
		decAmt := decimal.NewFromFloat(amt.Amount)
		if value, exist := relayerAmtsMap[key]; exist {
			value.Amt = value.Amt.Add(decAmt)
			value.Txs += amt.Count
			relayerAmtsMap[key] = value
		} else {
			relayerAmtsMap[key] = TxsAmtItem{
				Amt: decAmt,
				Txs: amt.Count,
			}
		}
	}
	return relayerAmtsMap
}

func createIBCRelayerStatistics(address, baseDenom, baseDenomChainId string, amount decimal.Decimal, successTxs, txs, startTime, endTime int64) entity.IBCRelayerStatistics {
	return entity.IBCRelayerStatistics{
		Address:           address,
		TransferBaseDenom: baseDenom,
		BaseDenomChainId:  baseDenomChainId,
		TransferAmount:    amount.String(),
		SuccessTotalTxs:   successTxs,
		TotalTxs:          txs,
		SegmentStartTime:  startTime,
		SegmentEndTime:    endTime,
		CreateAt:          time.Now().Unix(),
		UpdateAt:          time.Now().Unix(),
	}
}

func (t *IbcRelayerCronTask) caculateRelayerTotalValue() {
	baseDenomAmtDtos, err := relayerStatisticsRepo.CountRelayerBaseDenomAmt()
	if err != nil {
		logrus.Error("count relayer basedenom amount failed, ", err.Error())
		return
	}
	createAmtValue := func(baseDenomAmtDtos []*dto.CountRelayerBaseDenomAmtDTO) map[string]decimal.Decimal {
		relayerAmtValueMap := make(map[string]decimal.Decimal, 20)
		for _, amt := range baseDenomAmtDtos {
			key := relayerAmtValueMapKey(amt.StatisticId, amt.Address)
			decAmt := decimal.NewFromFloat(amt.Amount)
			baseDenomValue := decimal.NewFromFloat(0)
			if coin, ok := t.denomPriceMap[amt.BaseDenom+amt.BaseDenomChainId]; ok {
				if coin.Scale > 0 {
					baseDenomValue = decAmt.Div(decimal.NewFromFloat(math.Pow10(coin.Scale))).Mul(decimal.NewFromFloat(coin.Price))
				}
			}
			value, exist := relayerAmtValueMap[key]
			if exist {
				value = value.Add(baseDenomValue)
				relayerAmtValueMap[key] = value
			} else {
				relayerAmtValueMap[key] = baseDenomValue
			}
		}
		return relayerAmtValueMap
	}
	t.relayerValueMap = createAmtValue(baseDenomAmtDtos)
}

//dependence: caculateRelayerTotalValue, AggregateRelayerPacketTxs
func (t *IbcRelayerCronTask) saveOrUpdateRelayerTxsAndValue(val *entity.IBCRelayer) {
	getRelayerValue := func(data *entity.IBCRelayer) string {
		key1, key2 := relayerStatisticsRepo.CreateStatisticId(data.ChainA, data.ChainB, data.ChannelA, data.ChannelB)
		totalValue := decimal.NewFromFloat(0)
		if data.ChainAAddress != "" {
			keyA := relayerAmtValueMapKey(key1, data.ChainAAddress)
			keyB := relayerAmtValueMapKey(key2, data.ChainAAddress)
			totalAValue, _ := t.relayerValueMap[keyA]
			totalBValue, _ := t.relayerValueMap[keyB]
			totalValue = totalValue.Add(totalAValue).Add(totalBValue).Round(constant.DefaultValuePrecision)
		}
		if data.ChainBAddress != "" {
			keyA := relayerAmtValueMapKey(key1, data.ChainBAddress)
			keyB := relayerAmtValueMapKey(key2, data.ChainBAddress)
			totalAValue, _ := t.relayerValueMap[keyA]
			totalBValue, _ := t.relayerValueMap[keyB]
			totalValue = totalValue.Add(totalAValue).Add(totalBValue).Round(constant.DefaultValuePrecision)
		}
		return totalValue.String()
	}

	getRelayerTxs := func(data *entity.IBCRelayer) (int64, int64) {
		txsSuccess, txs := int64(0), int64(0)
		key1, key2 := relayerStatisticsRepo.CreateStatisticId(data.ChainA, data.ChainB, data.ChannelA, data.ChannelB)
		if data.ChainAAddress != "" {
			keyA := t.relayerTxsDataMapKey(key1, data.ChainAAddress)
			keyB := t.relayerTxsDataMapKey(key2, data.ChainAAddress)
			totalTxsAValue, _ := t.relayerTxsDataMap[keyA]
			totalTxsBValue, _ := t.relayerTxsDataMap[keyB]
			txsSuccess += totalTxsAValue.TxsSuccess + totalTxsBValue.TxsSuccess
			txs += totalTxsAValue.Txs + totalTxsBValue.Txs
		}
		if data.ChainBAddress != "" {
			keyA := t.relayerTxsDataMapKey(key1, data.ChainBAddress)
			keyB := t.relayerTxsDataMapKey(key2, data.ChainBAddress)
			totalTxsAValue, _ := t.relayerTxsDataMap[keyA]
			totalTxsBValue, _ := t.relayerTxsDataMap[keyB]
			txsSuccess += totalTxsAValue.TxsSuccess + totalTxsBValue.TxsSuccess
			txs += totalTxsAValue.Txs + totalTxsBValue.Txs
		}

		return txs, txsSuccess
	}

	totalValue := getRelayerValue(val)
	txs, txsSuccess := getRelayerTxs(val)
	if err := relayerRepo.UpdateTxsInfo(val.RelayerId, txs, txsSuccess, totalValue); err != nil && err != qmgo.ErrNoSuchDocuments {
		logrus.Error("update txs,txsSuccess,totalValue failed, ", err.Error())
	}

}

func (t *IbcRelayerCronTask) getChannelsStatus(chainId, dcChainId string) []*entity.ChannelPath {
	cfg, ok := t.chainConfigMap[chainId]
	if ok {
		for _, v := range cfg.IbcInfo {
			if v.ChainId == dcChainId {
				return v.Paths
			}
		}
	}

	return nil
}

func (t *IbcRelayerCronTask) updateIbcChainsRelayer() {
	res, err := chainCache.FindAll()
	if err != nil {
		logrus.Error("find ibc_chains data fail, ", err.Error())
		return
	}
	for _, val := range res {
		relayerCnt, err := relayerRepo.CountChainRelayers(val.ChainId)
		if err != nil {
			logrus.Error("count relayers of chain fail, ", err.Error())
			continue
		}
		if err := chainRepo.UpdateRelayers(val.ChainId, relayerCnt); err != nil {
			logrus.Error("update ibc_chain relayers fail, ", err.Error())
		}
	}
	return
}

func createRelayerData(dto *dto.GetRelayerInfoDTO) entity.IBCRelayer {
	return entity.IBCRelayer{
		ChainA:        dto.ScChainId,
		ChainB:        dto.DcChainId,
		ChannelA:      dto.ScChannel,
		ChannelB:      dto.DcChannel,
		ChainBAddress: dto.DcChainAddress,
		Status:        entity.ChannelStatusClosed,
		UpdateTime:    0,
		TimePeriod:    -1,
		CreateAt:      time.Now().Unix(),
		UpdateAt:      time.Now().Unix(),
	}
}

//1: timePeriod
//2: updateTime
//3: error
func (t *IbcRelayerCronTask) getTimePeriodAndupdateTime(relayer *entity.IBCRelayer) (int64, int64, int) {
	var updateTimeA, timePeriodA, updateTimeB, timePeriodB, startTime int64
	var clientIdA, clientIdB string
	var err error
	//use unbonding_time
	startTime = time.Now().Add(-24 * 21 * time.Hour).Unix()
	if relayer.UpdateTime > 0 && relayer.UpdateTime <= startTime {
		startTime = relayer.UpdateTime
	} else {
		unbondTime, _ := unbondTimeCache.GetUnbondTime(relayer.ChainA)
		if unbondTime != "" {
			if unbondTimeSeconds, err := strconv.ParseInt(strings.ReplaceAll(unbondTime, "s", ""), 10, 64); err == nil && unbondTimeSeconds > 0 && unbondTimeSeconds < startTime {
				startTime = time.Now().Add(time.Duration(-unbondTimeSeconds) * time.Second).Unix()
			}
		}
	}
	group := sync.WaitGroup{}
	group.Add(2)
	go func() {
		defer group.Done()
		clientIdA, err = t.getChannelClient(relayer.ChainA, relayer.ChannelA)
		if err != nil {
			timePeriodA = -1
			return
		}
		updateTimeA, timePeriodA, err = txRepo.GetTimePeriodByUpdateClient(relayer.ChainA, relayer.ChainAAddress, clientIdA, startTime)
		if err != nil {
			logrus.Warnf("get relayer timePeriod and updateTime fail, %s", err.Error())
		}
	}()

	go func() {
		defer group.Done()
		clientIdB, err = t.getChannelClient(relayer.ChainB, relayer.ChannelB)
		if err != nil {
			timePeriodB = -1
			return
		}
		updateTimeB, timePeriodB, err = txRepo.GetTimePeriodByUpdateClient(relayer.ChainB, relayer.ChainBAddress, clientIdB, startTime)
		if err != nil {
			logrus.Warnf("get relayer timePeriod and updateTime fail, %s", err.Error())
		}
	}()
	group.Wait()

	timePeriod := timePeriodB
	updateTime := updateTimeB
	if timePeriodA >= timePeriodB && timePeriodB > 0 {
		// 两条链对应timePeriodB均不为-1，表示均正常取最大基准周期
		timePeriod = timePeriodA
		if updateTimeA >= updateTimeB {
			updateTime = updateTimeA
		}
	} else if timePeriodA == timePeriodB && timePeriodB == -1 {
		// 两条链对应timePeriodB均为-1，表示均超过12h取update_client交易时间最小的做为relayer的更新时间
		if updateTimeA > 0 && updateTimeA < updateTimeB {
			updateTime = updateTimeA
		}
	} else if timePeriodA == -1 || timePeriodB == -1 {
		updateTime = relayer.UpdateTime
		if timePeriodA > 0 {
			timePeriod = timePeriodA
		}
	}
	//判断更新时间如果小于历史更新时间，就不更新
	if updateTime < relayer.UpdateTime {
		updateTime = relayer.UpdateTime
	}
	if updateTime == 0 && timePeriod == -1 && relayer.UpdateTime == 0 {
		updateTime = t.findLatestRecvPacketTime(relayer, startTime)
	}
	logrus.Debug("clientIdA: ", clientIdA, " clientIdB: ", clientIdB, "chainA: ", relayer.ChainA, "chainB: ", relayer.ChainB)
	//return timePeriod, updateTime, matchChannels(clientIdA, clientIdB, relayer)
	return timePeriod, updateTime, channelMatchSuccess
}

func (t *IbcRelayerCronTask) getChannelClient(chainId, channelId string) (string, error) {
	chainConf, ok := t.chainConfigMap[chainId]
	if !ok {
		return "", fmt.Errorf("%s config not found", chainId)
	}

	port := chainConf.GetPortId(channelId)
	state, err := queryClientState(chainConf.Lcd, chainConf.LcdApiPath.ClientStatePath, port, channelId)
	if err != nil {
		return "", err
	}

	return state.IdentifiedClientState.ClientId, nil
}

func (t *IbcRelayerCronTask) findLatestRecvPacketTime(relayer *entity.IBCRelayer, startTime int64) int64 {
	var (
		updateTimeA, updateTimeB int64
		err                      error
	)
	group := sync.WaitGroup{}
	group.Add(2)
	go func() {
		updateTimeA, err = txRepo.GetLatestRecvPacketTime(relayer.ChainA, relayer.ChainAAddress, relayer.ChannelA, startTime)
		if err != nil {
			logrus.Warn("get relayer timePeriod and updateTime fail" + err.Error())
		}
		group.Done()
	}()

	go func() {
		updateTimeB, err = txRepo.GetLatestRecvPacketTime(relayer.ChainB, relayer.ChainBAddress, relayer.ChannelB, startTime)
		if err != nil {
			logrus.Warn("get relayer timePeriod and updateTime fail" + err.Error())
		}
		group.Done()
	}()
	group.Wait()
	if updateTimeA > updateTimeB {
		return updateTimeA
	}
	return updateTimeB
}
func (t *IbcRelayerCronTask) todayStatistics() error {
	logrus.Infof("task %s exec today statistics", t.Name())
	startTime, endTime := todayUnix()
	segments := []*segment{
		{
			StartTime: startTime,
			EndTime:   endTime,
		},
	}
	if err := relayerStatisticsTask.deal(segments, opUpdate); err != nil {
		logrus.Errorf("task %s todayStatistics error, %v", t.Name(), err)
		return err
	}
	relayerDataTask.handleNewRelayerOnce(segments, false)

	return nil
}

func (t *IbcRelayerCronTask) yesterdayStatistics() error {
	mmdd := time.Now().Format(constant.TimeFormatMMDD)
	incr, _ := statisticsCheckRepo.GetIncr(t.Name(), mmdd)
	if incr > statisticsCheckTimes {
		return nil
	}

	logrus.Infof("task %s check yeaterday statistics, time: %d", t.Name(), incr)
	startTime, endTime := yesterdayUnix()
	segments := []*segment{
		{
			StartTime: startTime,
			EndTime:   endTime,
		},
	}
	if err := relayerStatisticsTask.deal(segments, opUpdate); err != nil {
		logrus.Errorf("task %s todayStatistics error, %v", t.Name(), err)
		return err
	}
	relayerDataTask.handleNewRelayerOnce(segments, false)

	_ = statisticsCheckRepo.Incr(t.Name(), mmdd)
	return nil
}

func checkAndUpdateRelayerSrcChainAddr() {
	skip := int64(0)
	limit := int64(500)
	startTimeA := time.Now().Unix()
	for {
		relayers, err := relayerRepo.FindEmptyAddrAll(skip, limit)
		if err != nil {
			logrus.Error("find relayer by page fail, ", err.Error())
			return
		}
		for _, relayer := range relayers {
			addrs := getSrcChainAddress(&dto.GetRelayerInfoDTO{
				ScChainId:      relayer.ChainA,
				ScChannel:      relayer.ChannelA,
				DcChainId:      relayer.ChainB,
				DcChannel:      relayer.ChannelB,
				DcChainAddress: relayer.ChainBAddress,
			}, false)
			historyAddrs := getSrcChainAddress(&dto.GetRelayerInfoDTO{
				ScChainId:      relayer.ChainA,
				ScChannel:      relayer.ChannelA,
				DcChainId:      relayer.ChainB,
				DcChannel:      relayer.ChannelB,
				DcChainAddress: relayer.ChainBAddress,
			}, true)
			addrs = append(addrs, historyAddrs...)
			if len(addrs) > 0 {
				addrs = utils.DistinctSliceStr(addrs)
				if err := relayerRepo.UpdateSrcAddress(relayer.RelayerId, addrs); err != nil && !qmgo.IsDup(err) {
					logrus.Error("Update Src Address failed, " + err.Error())
				}
			}

		}
		if len(relayers) < int(limit) {
			break
		}
		skip += limit
	}
	logrus.Infof("cronjob execute checkAndUpdateRelayerSrcChainAddr finished, time use %d(s)", time.Now().Unix()-startTimeA)
}
