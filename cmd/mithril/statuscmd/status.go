package statuscmd

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/sonicfromnewyoke/mithril/pkg/config"
	"github.com/sonicfromnewyoke/mithril/pkg/tui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	accountsPath string

	StatusCmd = cobra.Command{
		Use:   "status",
		Short: "Show current Mithril node status",
		Long:  "Check if Mithril is running, what slot it's at, and Lightbringer connectivity.",
		Run: func(cmd *cobra.Command, args []string) {
			runStatus()
		},
	}
)

func init() {
	StatusCmd.Flags().StringVar(&accountsPath, "accounts", "", "Path to AccountsDB directory (to find state file)")
}

var (
	titleStyle   = tui.TitleStyle
	successStyle = tui.SuccessStyle
	warnStyle    = tui.WarnStyle
	dimStyle     = tui.DimStyle
	valueStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
)

// mithrilState mirrors the relevant fields from pkg/state/state.go
type mithrilState struct {
	SnapshotSlot   uint64 `json:"snapshot_slot"`
	LastSlot       uint64 `json:"last_slot"`
	LastBankhash   string `json:"last_bankhash"`
	GenesisHash    string `json:"genesis_hash"`
	ShutdownReason string `json:"last_shutdown_reason"`
}

func runStatus() {
	fmt.Println()
	fmt.Println(titleStyle.Render("◎ Mithril Status"))
	fmt.Println()

	// Try to find state file
	stateFound := false
	searchPaths := []string{accountsPath}
	if accountsPath == "" {
		searchPaths = []string{
			config.DefaultStoragePaths().Accounts,
			"./data/accounts",
			".",
		}
	}

	var state mithrilState
	var statePath string
	for _, dir := range searchPaths {
		p := filepath.Join(dir, "mithril_state.json")
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if err := json.Unmarshal(data, &state); err != nil {
			continue
		}
		statePath = p
		stateFound = true
		break
	}

	if stateFound {
		fmt.Printf("  %s State file: %s\n", successStyle.Render("✓"), dimStyle.Render(statePath))
		fmt.Printf("  %s Last slot:  %s\n", successStyle.Render("✓"), valueStyle.Render(fmt.Sprintf("%d", state.LastSlot)))
		if state.SnapshotSlot > 0 {
			fmt.Printf("  %s Snapshot:   %s\n", dimStyle.Render("-"), valueStyle.Render(fmt.Sprintf("slot %d", state.SnapshotSlot)))
		}
		if state.ShutdownReason != "" {
			fmt.Printf("  %s Last stop:  %s\n", dimStyle.Render("-"), valueStyle.Render(state.ShutdownReason))
		}
		if state.LastBankhash != "" {
			short := state.LastBankhash
			if len(short) > 12 {
				short = short[:12] + "..."
			}
			fmt.Printf("  %s Bankhash:   %s\n", dimStyle.Render("-"), dimStyle.Render(short))
		}
	} else {
		fmt.Printf("  %s No state file found\n", warnStyle.Render("~"))
		fmt.Printf("    %s Mithril hasn't run yet, or --accounts path is wrong\n", dimStyle.Render(""))
	}

	fmt.Println()

	// Read service addresses from config (fall back to defaults)
	if err := config.InitConfig(); err != nil {
		fmt.Printf("  %s Failed to read config: %v\n", warnStyle.Render("~"), err)
		fmt.Println("  Using default service addresses")
	}
	rpcAddr := "127.0.0.1:8899"
	lbAddr := "127.0.0.1:3001"
	lbHTTP := "127.0.0.1:3000"
	if port := config.GetString("rpc.port"); port != "" && port != "0" {
		rpcAddr = "127.0.0.1:" + port
	}
	if addr := config.GetString("lightbringer.grpc_addr"); addr != "" {
		lbAddr = addr
	}
	if addr := config.GetString("lightbringer.rpc_addr"); addr != "" {
		lbHTTP = addr
	}
	blockSource := config.GetString("block.source")
	lightbringerEnabled := config.GetBool("lightbringer.enabled")
	effectiveBlockSource := blockSource
	if effectiveBlockSource == "" {
		effectiveBlockSource = "rpc"
	}
	// Match run-time behavior: enabling the managed sidecar promotes block delivery
	// to Lightbringer unless a CLI flag explicitly forced RPC.
	if lightbringerEnabled && effectiveBlockSource == "rpc" {
		effectiveBlockSource = "lightbringer"
	}

	// External LB mode: use block.lightbringer_endpoint if set
	if extAddr := config.GetString("block.lightbringer_endpoint"); extAddr != "" {
		lbAddr = extAddr
	}

	fmt.Println("  " + dimStyle.Render("Services:"))

	// Check Mithril RPC (skip if port=0 means disabled)
	if config.GetString("rpc.port") == "0" {
		fmt.Printf("  %s Mithril RPC disabled (port=0)\n", dimStyle.Render("-"))
	} else {
		conn, err := net.DialTimeout("tcp", rpcAddr, 2*time.Second)
		if err == nil {
			conn.Close()
			fmt.Printf("  %s Mithril RPC responding on %s\n", successStyle.Render("✓"), rpcAddr)
		} else {
			fmt.Printf("  %s Mithril RPC not responding on %s\n", dimStyle.Render("-"), rpcAddr)
		}
	}

	// Only probe Lightbringer when it is the effective block source.
	if effectiveBlockSource == "lightbringer" {
		conn, err := net.DialTimeout("tcp", lbAddr, 2*time.Second)
		if err == nil {
			conn.Close()
			fmt.Printf("  %s Lightbringer gRPC responding on %s\n", successStyle.Render("✓"), lbAddr)
		} else {
			fmt.Printf("  %s Lightbringer gRPC not responding on %s\n", dimStyle.Render("-"), lbAddr)
		}

		// Only probe HTTP when using managed sidecar (not external)
		if lightbringerEnabled {
			conn, err = net.DialTimeout("tcp", lbHTTP, 2*time.Second)
			if err == nil {
				conn.Close()
				fmt.Printf("  %s Lightbringer HTTP responding on %s\n", successStyle.Render("✓"), lbHTTP)
			} else {
				fmt.Printf("  %s Lightbringer HTTP not responding on %s\n", dimStyle.Render("-"), lbHTTP)
			}
			if config.GetBool("lightbringer.quiet") {
				fmt.Printf("  %s Lightbringer quiet mode: enabled (warn/error only)\n", dimStyle.Render("·"))
			}
		}
	}

	fmt.Println()
}
