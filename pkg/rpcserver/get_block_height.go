package rpcserver

import (
	"context"
	"fmt"

	"github.com/sonicfromnewyoke/mithril/pkg/global"
	"github.com/filecoin-project/go-jsonrpc"
)

func (rpcServer *RpcServer) GetBlockHeight(ctx context.Context, p jsonrpc.RawParams) (uint64, error) {
	params, err := jsonrpc.DecodeParams[[]interface{}](p)
	if err != nil {
		return 0, fmt.Errorf("decoding params: %w", err)
	}

	_ = params

	return global.BlockHeight(), nil
}
