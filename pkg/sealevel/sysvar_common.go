package sealevel

import "github.com/sonicfromnewyoke/mithril/pkg/accounts"

func addrObjectForLookup(execCtx *ExecutionCtx) *accounts.Accounts {
	if execCtx.SlotCtx != nil && execCtx.SlotCtx.Replay {
		return &execCtx.SlotCtx.Accounts
	} else {
		return &execCtx.Accounts
	}
}
