package cache

import (
	"fmt"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/utils"
)

//LcdAddrCacheRepo 缓存从lcd查询的交易相关信息
type LcdAddrCacheRepo struct {
}

type TraceSourceLcd struct {
	LcdAddr       string `json:"lcd_addr"`
	TxIndexEnable bool   `json:"tx_index_enable"`
	FullNode      bool   `json:"full_node"`
}

func (repo *LcdAddrCacheRepo) Set(chainId string, value []TraceSourceLcd) error {
	err := rc.Set(fmt.Sprintf(lcdAddr, chainId), string(utils.MarshalJsonIgnoreErr(value)), 7*oneDay)
	return err
}

func (repo *LcdAddrCacheRepo) Get(chainId string) ([]TraceSourceLcd, error) {
	var res []TraceSourceLcd
	value, err := rc.Get(fmt.Sprintf(lcdAddr, chainId))
	if err != nil {
		return nil, err
	}
	utils.UnmarshalJsonIgnoreErr([]byte(value), &res)
	return res, nil
}
