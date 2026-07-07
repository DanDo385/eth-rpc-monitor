package main

import (
	"github.com/spf13/cobra"

	"github.com/dando385/eth-rpc-monitor/internal/cli"
)

// blockCmd implements `ethrpc block [block]` — fetch and display one block.
// Defaults to "latest" with auto-selected fastest provider.
var blockCmd = &cobra.Command{
	Use:   "block [block]",
	Short: "Inspect a single block from one provider",
	Long: "Fetch and display a single Ethereum block. With no --provider, all " +
		"providers are raced via eth_blockNumber and the fastest one on the " +
		"highest head is auto-selected.\n\n" +
		"The block argument accepts a decimal number, hex (0x...), or a tag " +
		"(latest, pending, earliest). Defaults to \"latest\".",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		blockArg := "latest"
		if len(args) > 0 {
			blockArg = cli.NormalizeBlockArg(args[0])
		}
		provider, err := cmd.Flags().GetString("provider")
		if err != nil {
			return err
		}
		jsonOut, err := cmd.Flags().GetBool("json")
		if err != nil {
			return err
		}
		return cli.RunBlock(loadedCfg, blockArg, provider, jsonOut)
	},
}

func init() {
	blockCmd.Flags().String("provider", "", "Use specific provider (empty = auto-select fastest)")
	blockCmd.Flags().BoolP("json", "j", false, "Output JSON report to reports directory")
}
