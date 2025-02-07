package task

import (
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/constant"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/model/entity"
	"github.com/qiniu/qmgo"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"sync"
)

type FixAcknowledgeTxTask struct {
}

var _ OneOffTask = new(FixAcknowledgeTxTask)

func (t *FixAcknowledgeTxTask) Name() string {
	return "fix_acknowledge_tx_task"
}

func (t *FixAcknowledgeTxTask) Switch() bool {
	return false
}

func (t *FixAcknowledgeTxTask) Run() int {
	segments, err := getSegment(segmentStepLatest)
	if err != nil {
		logrus.Errorf("task %s getSegment error, %v", t.Name(), err)
		return -1
	}

	historySegments, err := getHistorySegment(segmentStepHistory)
	if err != nil {
		logrus.Errorf("task %s getHistorySegment error, %v", t.Name(), err)
		return -1
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		err := t.fixAcknowledgeTxs(ibcTxTargetLatest, segments)
		logrus.Infof("task %s fix latest end, %v", t.Name(), err)
	}()

	go func() {
		defer wg.Done()
		err := t.fixAcknowledgeTxs(ibcTxTargetHistory, historySegments)
		logrus.Infof("task %s fix history end, %v", t.Name(), err)
	}()

	wg.Wait()
	return 1
}

func (t *FixAcknowledgeTxTask) fixAcknowledgeTxs(target string, segments []*segment) error {
	const limit int64 = 1000
	isTargetHistory := false
	if target == ibcTxTargetHistory {
		isTargetHistory = true
	}
	doHandleSegments(t.Name(), 5, segments, isTargetHistory, func(seg *segment, isTargetHistory bool) {
		var skip int64 = 0
		for {
			txs, err := ibcTxRepo.FindAcknowledgeTxsEmptyTxs(seg.StartTime, seg.EndTime, skip, limit, isTargetHistory)
			if err != nil {
				logrus.Errorf("task %s FindAcknowledgeTxsEmptyTxs %s %d-%d err, %v", t.Name(), target, seg.StartTime, seg.EndTime, err)
				return
			}

			for _, val := range txs {
				err := t.SaveAcknowledgeTx(val, isTargetHistory)
				if err != nil && err != qmgo.ErrNoSuchDocuments {
					logrus.Errorf("task %s saveAcknowledgeTx %s err, chain_id: %s, packet_id: %s, %v", t.Name(), target, val.ScChainId, val.ScTxInfo.Msg.CommonMsg().PacketId, err)
					return
				}
			}

			if int64(len(txs)) < limit {
				break
			}
			skip += limit
		}
	})

	return nil
}

func (t *FixAcknowledgeTxTask) SaveAcknowledgeTx(ibcTx *entity.ExIbcTx, history bool) error {
	packetId := ibcTx.ScTxInfo.Msg.CommonMsg().PacketId
	ackTxs, err := txRepo.GetAcknowledgeTxs(ibcTx.ScChainId, packetId)
	if err != nil {
		return err
	}
	if len(ackTxs) > 0 {
		if ibcTx.Status == entity.IbcTxStatusSuccess {
			//"成功"状态IBC，第三段取最新的ack tx交易
			ackTx := ackTxs[0]
			ibcTx.RefundedTxInfo = &entity.TxInfo{
				Hash:      ackTx.TxHash,
				Height:    ackTx.Height,
				Time:      ackTx.Time,
				Status:    ackTx.Status,
				Fee:       ackTx.Fee,
				Memo:      ackTx.Memo,
				Signers:   ackTx.Signers,
				MsgAmount: nil,
				Msg:       getMsgByType(*ackTx, constant.MsgTypeAcknowledgement, packetId),
			}
			return ibcTxRepo.UpdateOne(ibcTx.RecordId, history, bson.M{
				"$set": bson.M{
					"refunded_tx_info": ibcTx.RefundedTxInfo,
				},
			})
		}
	}
	return nil
}
