package rest

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/api/response"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/repository/cache"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type TaskController struct {
}

func (ctl *TaskController) Run(c *gin.Context) {
	taskName := c.Param("task_name")
	if taskName == "" {
		c.JSON(http.StatusOK, response.FailBadRequest(fmt.Errorf("task name is empty")))
		return
	}
	lockKey := fmt.Sprintf("%s:%s", "TaskController", taskName)
	if err := cache.GetRedisClient().Lock(lockKey, time.Now().Unix(), time.Hour); err != nil {
		c.JSON(http.StatusTooManyRequests, response.FailMsg("Please try again later"))
		return
	}

	go func() {
		st := time.Now().Unix()
		res := 0
		logrus.Infof("TaskController task %s start", taskName)

		switch taskName {
		case addChainTask.Name():
			res = addChainTask.RunWithParam(c.PostForm("new_chains"))
		case fixDcChainIdTask.Name():
			res = fixDcChainIdTask.Run()
		case fixBaseDenomChainIdTask.Name():
			res = fixBaseDenomChainIdTask.Run()
		case fixDenomTraceDataTask.Name():
			startTime, err := strconv.ParseInt(c.PostForm("start_time"), 10, 64)
			if err != nil {
				logrus.Errorf("TaskController run %s err, %v", taskName, err)
				return
			}
			endTime, err := strconv.ParseInt(c.PostForm("start_time"), 10, 64)
			if err != nil {
				logrus.Errorf("TaskController run %s err, %v", taskName, err)
				return
			}
			res = fixDenomTraceDataTask.RunWithParam(startTime, endTime)
		case fixDenomTraceHistoryDataTask.Name():
			startTime, err := strconv.ParseInt(c.PostForm("start_time"), 10, 64)
			if err != nil {
				logrus.Errorf("TaskController run %s err, %v", taskName, err)
				return
			}
			endTime, err := strconv.ParseInt(c.PostForm("start_time"), 10, 64)
			if err != nil {
				logrus.Errorf("TaskController run %s err, %v", taskName, err)
				return
			}
			res = fixDenomTraceHistoryDataTask.RunWithParam(startTime, endTime)
		case tokenStatisticsTask.Name():
			res = tokenStatisticsTask.Run()
		case channelStatisticsTask.Name():
			res = channelStatisticsTask.Run()
		case relayerStatisticsTask.Name():
			res = relayerStatisticsTask.Run()
		case relayerDataTask.Name():
			res = relayerDataTask.Run()
		case fixFailRecvPacketTask.Name():
			fixFailRecvPacketTask.Run()
		case addTransferDataTask.Name():
			addTransferDataTask.RunWithParam(c.PostForm("new_chains"))
		case fixFailTxTask.Name():
			value := c.PostForm("start_time")
			if len(value) > 0 {
				startTime, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					logrus.Errorf("TaskController run %s err, %v", taskName, err)
					return
				}
				fixFailTxTask.RunWithParam(startTime)
			} else {
				fixFailTxTask.Run()
			}

		case fixAcknowledgeTxTask.Name():
			fixAcknowledgeTxTask.Run()
		case fixAckTxPacketIdTask.Name():
			fixAckTxPacketIdTask.RunWithParam(c.PostForm("chains"), c.PostForm("end_height"))
		case fixIbxTxTask.Name():
			fixIbxTxTask.RunWithParam(c.PostForm("domain"))
		case ibcNodeLcdCronTask.Name():
			value := c.PostForm("chains")
			if len(value) > 0 {
				ibcNodeLcdCronTask.RunWithParam(value)
			} else {
				ibcNodeLcdCronTask.Run()
			}
		case ibcStatisticCronTask.Name():
			ibcStatisticCronTask.NewRun()
		default:
			logrus.Errorf("TaskController run %s err, %s", taskName, "unknown task")
		}

		logrus.Infof("TaskController task %s end, time use %d(s), exec status: %d", taskName, time.Now().Unix()-st, res)
	}()
	time.Sleep(1 * time.Second)
	c.JSON(http.StatusOK, response.Success("task is running"))

}
