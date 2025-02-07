package task

import (
	"fmt"
	"strings"
	"sync"

	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/constant"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/global"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/model/entity"
	"github.com/sirupsen/logrus"
)

type AddChainTask struct {
}

var _ OneOffTask = new(AddChainTask)

func (t *AddChainTask) Name() string {
	return "add_chain_task"
}

func (t *AddChainTask) Switch() bool {
	return global.Config.Task.SwitchAddChainTask
}

func (t *AddChainTask) Run() int {
	chainsStr := global.Config.ChainConfig.NewChains
	newChainIds := strings.Split(chainsStr, ",")
	if len(newChainIds) == 0 {
		logrus.Errorf("task %s don't have new chains", t.Name())
		return 1
	}

	return t.handle(newChainIds)
}

func (t *AddChainTask) RunWithParam(chainsStr string) int {
	newChainIds := strings.Split(chainsStr, ",")
	if len(newChainIds) == 0 {
		logrus.Errorf("task %s don't have new chains", t.Name())
		return 1
	}

	return t.handle(newChainIds)
}

func (t *AddChainTask) handle(newChainIds []string) int {
	chainMap, err := getAllChainMap()
	if err != nil {
		logrus.Errorf("task %s getAllChainMap error, %v", t.Name(), err)
		return -1
	}

	denomList, err := denomRepo.FindAll()
	if err != nil {
		logrus.Errorf("task %s denomRepo.FindAll error, %v", t.Name(), err)
		return -1
	}

	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	// update ibc tx
	go func() {
		defer waitGroup.Done()
		for _, chainId := range newChainIds {
			chainConfig, ok := chainMap[chainId]
			if !ok {
				logrus.Warningf("task %s %s dont't have chain config", t.Name(), chainId)
				continue
			}

			t.updateIbcTx(chainId, chainConfig, chainMap)
		}
	}()

	// update denom
	go func() {
		defer waitGroup.Done()
		t.updateDenom(denomList, chainMap)
	}()

	waitGroup.Wait()
	return 1
}

func (t *AddChainTask) updateIbcTx(chainId string, chainConfig *entity.ChainConfig, chainMap map[string]*entity.ChainConfig) {
	logrus.Infof("task %s start updating %s ibc tx", t.Name(), chainId)
	if len(chainConfig.IbcInfo) == 0 {
		logrus.Warningf("task %s %s dont't have ibc info", t.Name(), chainId)
		return
	}

	for _, ibcInfo := range chainConfig.IbcInfo {
		for _, path := range ibcInfo.Paths {
			if path.State != constant.ChannelStateOpen {
				logrus.Warningf("task %s %s channel %s is not open", t.Name(), chainId, path.ChannelId)
				continue
			}

			clientId := path.ClientId
			var counterpartyClientId string
			counterpartyChainId := path.ChainId
			counterpartyChannelId := path.Counterparty.ChannelId
			cpChainCfg, ok := chainMap[counterpartyChainId]
			if ok {
				counterpartyClientId = cpChainCfg.GetChannelClient(constant.PortTransfer, counterpartyChannelId)
			}

			channelId := path.ChannelId
			var waitGroup sync.WaitGroup
			waitGroup.Add(4)
			go func() {
				defer waitGroup.Done()
				if err := ibcTxRepo.AddNewChainUpdate(counterpartyChainId, counterpartyChannelId, counterpartyClientId, chainId, clientId); err != nil {
					logrus.Errorf("task %s %s AddNewChainUpdate error, counterpartyChainId: %s, counterpartyChannelId: %s", t.Name(), chainId, counterpartyChainId, counterpartyChannelId)
					_ = storageCache.AddChainError(chainId, counterpartyChainId, counterpartyChannelId)
				}
			}()

			go func() {
				defer waitGroup.Done()
				if err := ibcTxRepo.AddNewChainUpdateFailedTx(counterpartyChainId, counterpartyChannelId, counterpartyClientId, chainId, channelId, clientId); err != nil {
					logrus.Errorf("task %s %s AddNewChainUpdateFailedTx error, counterpartyChainId: %s, counterpartyChannelId: %s", t.Name(), chainId, counterpartyChainId, counterpartyChannelId)
					_ = storageCache.AddChainError(chainId, counterpartyChainId, counterpartyChannelId)
				}
			}()

			go func() {
				defer waitGroup.Done()
				if err := ibcTxRepo.AddNewChainUpdateHistory(counterpartyChainId, counterpartyChannelId, counterpartyClientId, chainId, clientId); err != nil {
					logrus.Errorf("task %s %s AddNewChainUpdateHistory error, counterpartyChainId: %s, counterpartyChannelId: %s", t.Name(), chainId, counterpartyChainId, counterpartyChannelId)
					_ = storageCache.AddChainError(chainId, counterpartyChainId, counterpartyChannelId)
				}
			}()

			go func() {
				defer waitGroup.Done()
				if err := ibcTxRepo.AddNewChainUpdateHistoryFailedTx(counterpartyChainId, counterpartyChannelId, counterpartyClientId, chainId, channelId, clientId); err != nil {
					logrus.Errorf("task %s %s AddNewChainUpdateHistoryFailedTx error, counterpartyChainId: %s, counterpartyChannelId: %s", t.Name(), chainId, counterpartyChainId, counterpartyChannelId)
					_ = storageCache.AddChainError(chainId, counterpartyChainId, counterpartyChannelId)
				}
			}()

			waitGroup.Wait()
		}
	}

	logrus.Infof("task %s update %s ibc tx end", t.Name(), chainId)
}

func (t *AddChainTask) updateDenom(denomList entity.IBCDenomList, chainMap map[string]*entity.ChainConfig) {
	logrus.Infof("task %s update denom start", t.Name())

	for _, v := range denomList {
		if v.DenomPath == "" || v.RootDenom == "" {
			continue
		}

		denomFullPath := fmt.Sprintf("%s/%s", v.DenomPath, v.RootDenom)
		denomNew := traceDenom(denomFullPath, v.ChainId, chainMap)
		if v.BaseDenom != denomNew.BaseDenom || v.BaseDenomChainId != denomNew.BaseDenomChainId || v.PrevDenom != denomNew.PrevDenom ||
			v.PrevChainId != denomNew.PrevChainId || v.IsBaseDenom != denomNew.IsBaseDenom {
			logrus.WithField("denom", v).WithField("denom_new", denomNew).Infof("task %s denom trace path is changed", t.Name())
			if err := denomRepo.UpdateDenom(denomNew); err != nil {
				logrus.Errorf("task %s update denom %s-%s error, %v", t.Name(), denomNew.ChainId, denomNew.Denom, err)
			}
		}

		if v.BaseDenom != denomNew.BaseDenom || v.BaseDenomChainId != denomNew.BaseDenomChainId {
			if err := ibcTxRepo.UpdateBaseDenomInfo(v.BaseDenom, v.BaseDenomChainId, denomNew.BaseDenom, denomNew.BaseDenomChainId); err != nil {
				logrus.Errorf("task %s UpdateBaseDenomInfo error, %s-%s => %s-%s", t.Name(), v.BaseDenomChainId, v.BaseDenom, denomNew.BaseDenomChainId, denomNew.BaseDenom)
				_ = storageCache.UpdateBaseDenomError(v.BaseDenom, v.BaseDenomChainId, denomNew.BaseDenom, denomNew.BaseDenomChainId)
			}
			if err := ibcTxRepo.UpdateBaseDenomInfoHistory(v.BaseDenom, v.BaseDenomChainId, denomNew.BaseDenom, denomNew.BaseDenomChainId); err != nil {
				logrus.Errorf("task %s UpdateBaseDenomInfoHistory error, %s-%s => %s-%s", t.Name(), v.BaseDenomChainId, v.BaseDenom, denomNew.BaseDenomChainId, denomNew.BaseDenom)
				_ = storageCache.UpdateBaseDenomError(v.BaseDenom, v.BaseDenomChainId, denomNew.BaseDenom, denomNew.BaseDenomChainId)
			}
		}
	}

	logrus.Infof("task %s update denom end", t.Name())
}
