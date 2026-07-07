package main

import (
	"github.com/spf13/cobra"

	"github.com/dando385/eth-rpc-monitor/internal/cli"
)

// snapshotCmd implements `ethrpc snapshot [block]` — fetch the same block from
// every provider and compare hash/height to detect disagreement.
var snapshotCmd = &cobra.Command{
	Use:   "snapshot [block]",
	Short: "Compare the same block across all providers (fork detection)",
	Long: "Concurrently fetch the given block from every provider and compare " +
		"the returned hash and height. Mismatches may indicate a stale or " +
		"forked provider.\n\n" +
		"The block argument is NOT normalized here: prefer \"latest\" or hex " +
		"(0x...). Decimal tags are not converted the way `block` does — use " +
		"`ethrpc block` for flexible decimal input. Defaults to \"latest\".",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		blockArg := "latest"
		if len(args) > 0 {
			blockArg = args[0]
		}
		return cli.RunSnapshot(loadedCfg, blockArg)
	},
}

func init() {
	// snapshot intentionally has no flags beyond the persistent --config.
}
