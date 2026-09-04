package rpcserver

import (
	"context"
	"fmt"

	"github.com/sonicfromnewyoke/mithril/pkg/global"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/mr-tron/base58"
)

type GetLatestBlockhashRespContext struct {
	Slot uint64 `json:"slot"`
}

type GetLatestBlockhashRespValue struct {
	Blockhash            string `json:"blockhash"`
	LastValidBlockHeight uint64 `json:"lastValidBlockHeight"`
}

type GetLatestBlockhashResp struct {
	Context GetLatestBlockhashRespContext `json:"context"`
	Value   *GetLatestBlockhashRespValue  `json:"value"`
}

func (rpcServer *RpcServer) GetLatestBlockhash(ctx context.Context, p jsonrpc.RawParams) (GetLatestBlockhashResp, error) {
	params, err := jsonrpc.DecodeParams[[]interface{}](p)
	if err != nil {
		return GetLatestBlockhashResp{}, fmt.Errorf("decoding params: %w", err)
	}

	_ = params

	latestBlockhash := global.LatestBlockHash()
	latestBlockhashStr := base58.Encode(latestBlockhash[:])
	lastValidBlockheight := global.BlockHeight() + 150

	val := &GetLatestBlockhashRespValue{Blockhash: latestBlockhashStr, LastValidBlockHeight: lastValidBlockheight}
	return GetLatestBlockhashResp{Context: GetLatestBlockhashRespContext{Slot: global.Slot()}, Value: val}, nil
}
