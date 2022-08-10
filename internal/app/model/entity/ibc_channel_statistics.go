package entity

type IBCChannelStatistics struct {
	ChannelId        string `bson:"channel_id"`
	BaseDenom        string `bson:"base_denom"`
	BaseDenomChainId string `bson:"base_denom_chain_id"`
	TransferTxs      int64  `bson:"transfer_txs"`
	TransferAmount   string `bson:"transfer_amount"`
	SegmentStartTime int64  `bson:"segment_start_time"`
	SegmentEndTime   int64  `bson:"segment_end_time"`
	CreateAt         int64  `bson:"create_at"`
	UpdateAt         int64  `bson:"update_at"`
}

func (i IBCChannelStatistics) CollectionName() string {
	return "ibc_channel_statistics"
}
