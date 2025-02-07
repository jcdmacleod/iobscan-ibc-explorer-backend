package entity

type RelayerStatus int

const (
	RelayerRunning RelayerStatus = 1
	RelayerStop    RelayerStatus = 2

	RelayerStopStr    = "Unknown"
	RelayerRunningStr = "Running"
)

type IBCRelayer struct {
	RelayerId             string        `bson:"relayer_id"`
	ChainA                string        `bson:"chain_a"`
	ChainB                string        `bson:"chain_b"`
	ChannelA              string        `bson:"channel_a"`
	ChannelB              string        `bson:"channel_b"`
	ChainAAddress         string        `bson:"chain_a_address"`
	ChainAAllAddress      []string      `bson:"chain_a_all_address"`
	ChainBAddress         string        `bson:"chain_b_address"`
	TimePeriod            int64         `bson:"time_period"`
	Status                RelayerStatus `bson:"status"`
	UpdateTime            int64         `bson:"update_time"`
	TransferTotalTxs      int64         `bson:"transfer_total_txs"`
	TransferSuccessTxs    int64         `bson:"transfer_success_txs"`
	TransferTotalTxsValue string        `bson:"transfer_total_txs_value"`
	CreateAt              int64         `bson:"create_at"`
	UpdateAt              int64         `bson:"update_at"`
}

func (i IBCRelayer) CollectionName() string {
	return "ibc_relayer"
}

func (i IBCRelayer) Valid() bool {
	return i.ChainA != "" && i.ChainB != "" && i.ChannelA != "" && i.ChannelB != "" && (i.ChainBAddress != "" || i.ChainAAddress != "")
}
