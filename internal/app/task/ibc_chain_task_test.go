package task

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/conf"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/global"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/model/entity"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/repository"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/repository/cache"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/utils"
	"github.com/sirupsen/logrus"
)

func TestMain(m *testing.M) {
	cache.InitRedisClient(conf.Redis{
		Addrs:    "127.0.0.1:6379",
		User:     "",
		Password: "",
		Mode:     "single",
		Db:       0,
	})
	repository.InitMgo(conf.Mongo{
		//Url: "mongodb://ibc:ibcpassword@192.168.0.122:27017,192.168.0.126:27017,192.168.0.127:27017/?authSource=iobscan-ibc",
		//Url: "mongodb://ibcreader:idy45Eth@35.229.186.42:27017/?connect=direct&authSource=iobscan-ibc",
		Url: "mongodb://iobscan:iobscanPassword@192.168.150.40:27017/?connect=direct&authSource=iobscan-ibc_0805",

		Database: "iobscan-ibc_0805",
	}, context.Background())

	time.Local = time.UTC
	global.Config = &conf.Config{
		Task: conf.Task{
			SingleChainSyncTransferTxMax:      1000,
			SingleChainIbcTxRelateMax:         1000,
			FixDenomTraceDataStartTime:        1634232199,
			FixDenomTraceDataEndTime:          1660103712,
			FixDenomTraceHistoryDataStartTime: 1620369550,
			FixDenomTraceHistoryDataEndTime:   1658830692,
		},
		ChainConfig: conf.ChainConfig{
			NewChains: "qa_iris_snapshot"}}
	m.Run()
}

func TestRunOnce(t *testing.T) {
	new(IbcChainCronTask).Run()
}

func Test_CheckFollowingStatus(t *testing.T) {
	chainList, err := chainConfigRepo.FindAll()
	if err != nil {
		t.Fatal(err)
	}

	w := new(syncTransferTxWorker)
	var notFollowingStatus []string

	for _, v := range chainList {
		checkFollowingStatus, err := w.checkFollowingStatus(v.ChainId)
		if err != nil {
			t.Fatal(err)
		}
		if !checkFollowingStatus {
			notFollowingStatus = append(notFollowingStatus, v.ChainId)
			//logrus.Warningf("chain %s is not follow status", v.ChainId)
		}
	}

	t.Log("chain is not follow status:")
	t.Log(utils.MustMarshalJsonToStr(notFollowingStatus))
}

func Test_CheckTransferStatus(t *testing.T) {
	chainList, err := chainConfigRepo.FindAll()
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range chainList {
		taskRecord, err := taskRecordRepo.FindByTaskName(fmt.Sprintf(entity.TaskNameFmt, v.ChainId))
		if err != nil {
			t.Fatal(err)
		}

		block, err := syncBlockRepo.FindLatestBlock(v.ChainId)
		if err != nil {
			t.Fatal(err)
		}

		if block.Height-taskRecord.Height > 20 {
			logrus.Warningf("chain %s trasnfer fall behind, latest block: %d, transfer block: %d", v.ChainId, block.Height, taskRecord.Height)
		}
	}
}
