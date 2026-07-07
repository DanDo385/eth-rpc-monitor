package main

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/dando385/eth-rpc-monitor/internal/cli"
)

// monitorCmd implements `ethrpc monitor` — a live, refreshing dashboard loop.
// Runs until Ctrl+C / SIGTERM. With --interval 0, uses the config default.
var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Live dashboard — refresh provider block heights and latency",
	Long: "Continuously poll every provider's eth_blockNumber and render a " +
		"refreshing dashboard showing block height, latency, and lag relative " +
		"to the leader. Exits cleanly on Ctrl+C or SIGTERM. With --interval 0, " +
		"uses the config watch_interval.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		interval, err := cmd.Flags().GetDuration("interval")
		if err != nil {
			return err
		}
		return cli.RunMonitor(loadedCfg, interval)
	},
}

func init() {
	monitorCmd.Flags().DurationP("interval", "i", 0*time.Second, "Refresh interval (0 = use config default)")
}
