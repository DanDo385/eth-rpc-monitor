package main

import (
	"github.com/spf13/cobra"

	"github.com/dando385/eth-rpc-monitor/internal/cli"
)

// testCmd implements `ethrpc test` — concurrent per-provider latency sampling
// with P50/P95/P99/Max percentiles.
var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Health check — latency samples and tail percentiles per provider",
	Long: "Run N concurrent eth_blockNumber samples against each provider, then " +
		"compute P50/P95/P99/Max. Includes a discarded warm-up call per provider " +
		"(not counted in stats). With --samples 0, uses the config default.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		samples, err := cmd.Flags().GetInt("samples")
		if err != nil {
			return err
		}
		jsonOut, err := cmd.Flags().GetBool("json")
		if err != nil {
			return err
		}
		return cli.RunTest(loadedCfg, samples, jsonOut)
	},
}

func init() {
	testCmd.Flags().IntP("samples", "s", 0, "Number of test samples per provider (0 = use config default)")
	testCmd.Flags().BoolP("json", "j", false, "Output JSON report to reports directory")
}
