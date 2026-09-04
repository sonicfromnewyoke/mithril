//go:build !lite

package blockstore

import (
	"github.com/sonicfromnewyoke/mithril/cmd/radiance/blockstore/compact"
	"github.com/sonicfromnewyoke/mithril/cmd/radiance/blockstore/dumpbatches"
	"github.com/sonicfromnewyoke/mithril/cmd/radiance/blockstore/dumpshreds"
	"github.com/sonicfromnewyoke/mithril/cmd/radiance/blockstore/statdatarate"
	"github.com/sonicfromnewyoke/mithril/cmd/radiance/blockstore/statentries"
	"github.com/sonicfromnewyoke/mithril/cmd/radiance/blockstore/verifydata"
	"github.com/sonicfromnewyoke/mithril/cmd/radiance/blockstore/yaml"
	"github.com/spf13/cobra"
)

var Cmd = cobra.Command{
	Use:   "blockstore",
	Short: "Access blockstore database",
}

func init() {
	Cmd.AddCommand(
		&compact.Cmd,
		&dumpshreds.Cmd,
		&dumpbatches.Cmd,
		&statdatarate.Cmd,
		&statentries.Cmd,
		&verifydata.Cmd,
		&yaml.Cmd,
	)
}
