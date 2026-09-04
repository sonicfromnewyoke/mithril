package gossip

import (
	"github.com/sonicfromnewyoke/mithril/cmd/radiance/gossip/ping"
	"github.com/sonicfromnewyoke/mithril/cmd/radiance/gossip/pull"
	"github.com/spf13/cobra"
)

var Cmd = cobra.Command{
	Use:   "gossip",
	Short: "Interact with Solana gossip networks",
}

func init() {
	Cmd.AddCommand(
		&ping.Cmd,
		&pull.Cmd,
	)
}
