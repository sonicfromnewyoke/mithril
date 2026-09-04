package rpcserver

import (
	"context"
	"fmt"

	"github.com/sonicfromnewyoke/mithril/pkg/global"
	"github.com/filecoin-project/go-jsonrpc"
)

type GetEpochInfoResp struct {
	AbsoluteSlot     uint64 `json:"absoluteSlot"`
	BlockHeight      uint64 `json:"blockHeight"`
	Epoch            uint64 `json:"epoch"`
	SlotIndex        uint64 `json:"slotIndex"`
	SlotsInEpoch     uint64 `json:"slotsInEpoch"`
	TransactionCount uint64 `json:"transactionCount"`
}

func (rpcServer *RpcServer) GetEpochInfo(ctx context.Context, p jsonrpc.RawParams) (GetEpochInfoResp, error) {
	params, err := jsonrpc.DecodeParams[[]interface{}](p)
	if err != nil {
		return GetEpochInfoResp{}, fmt.Errorf("decoding params: %w", err)
	}

	_ = params

	epoch := global.Epoch()
	slot := global.Slot()
	firstSlotInEpoch := rpcServer.epochSchedule.FirstSlotInEpoch(epoch)
	slotIndex := slot - firstSlotInEpoch

	resp := GetEpochInfoResp{
		AbsoluteSlot:     global.Slot(),
		BlockHeight:      global.BlockHeight(),
		Epoch:            global.Epoch(),
		SlotIndex:        slotIndex,
		SlotsInEpoch:     432000,
		TransactionCount: global.TransactionCount(),
	}

	return resp, nil
}
