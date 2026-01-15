package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmagro/eth-rpc-monitor/internal/output"
	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
)

func callCmd() *cobra.Command {
	var (
		rawOutput    bool
		providerName string
		format       string
	)

	cmd := &cobra.Command{
		Use:   "call",
		Short: "Query smart contract state",
		Long:  "Execute eth_call to read data from smart contracts.",
	}

	// Subcommand: call usdc
	usdcCmd := &cobra.Command{
		Use:   "usdc",
		Short: "Query USDC contract",
	}

	// Subcommand: call usdc balance
	balanceCmd := &cobra.Command{
		Use:   "balance ",
		Short: "Get USDC balance for an address",
		Long: `Query the USDC balance of an Ethereum address.

Examples:
  monitor call usdc balance 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045
  monitor call usdc balance 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045 --raw`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			if cfgPath == "" {
				cfgPath, _ = cmd.Root().PersistentFlags().GetString("config")
			}
			return runCallBalance(args[0], rpc.USDCAddress, "USDC", rpc.USDCDecimals, rawOutput, providerName, format, cfgPath)
		},
	}

	balanceCmd.Flags().BoolVar(&rawOutput, "raw", false, "Show raw calldata and response")
	balanceCmd.Flags().StringVar(&providerName, "provider", "", "Use specific provider")
	balanceCmd.Flags().StringVar(&format, "format", "terminal", "Output format: terminal|json")

	usdcCmd.AddCommand(balanceCmd)
	cmd.AddCommand(usdcCmd)

	return cmd
}

func runCallBalance(address, contract, symbol string, decimals int, rawOutput bool, providerName string, format string, cfgPath string) error {
	// Validate address
	if err := rpc.ValidateAddress(address); err != nil {
		return fmt.Errorf("invalid address: %w", err)
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	client, usedProvider, err := getProviderClient(cfg, providerName)
	if err != nil {
		return err
	}

	// Encode calldata
	calldata, err := rpc.EncodeBalanceOfCalldata(address)
	if err != nil {
		return fmt.Errorf("failed to encode calldata: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Defaults.Timeout)
	defer cancel()

	start := time.Now()
	rawResult, result := client.EthCall(ctx, contract, calldata, "latest")
	latency := time.Since(start)

	if !result.Success {
		return fmt.Errorf("eth_call failed: %v", result.Error)
	}

	// Decode result
	balance, err := rpc.DecodeUint256(rawResult)
	if err != nil {
		return fmt.Errorf("failed to decode balance: %w", err)
	}

	cd := &output.CallDisplay{
		Contract:       contract,
		ContractName:   symbol,
		Method:         "balanceOf",
		Address:        address,
		RawResult:      rawResult,
		Calldata:       calldata,
		ParsedValue:    balance,
		Decimals:       decimals,
		Symbol:         symbol,
		Provider:       usedProvider,
		Latency:        latency,
		IsAutoSelected: providerName == "",
	}

	if format == "json" {
		output.DisableColors()
		return output.RenderCallJSON(cd, rawOutput)
	}

	output.RenderCallTerminal(cd, rawOutput)
	return nil
}
