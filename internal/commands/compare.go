package commands

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/display"
	"github.com/dando385/eth-rpc-monitor/internal/provider"
	"github.com/dando385/eth-rpc-monitor/internal/report"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
	"github.com/dando385/eth-rpc-monitor/internal/util"
)

func RunCompare(cfg *config.Config, blockArg string, jsonOut bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Defaults.Timeout*2)
	defer cancel()

	blockNum := util.NormalizeBlockArg(blockArg)
	fmt.Printf("\nFetching block %s from %d providers...\n\n", blockArg, len(cfg.Providers))

	results := provider.ExecuteAll(ctx, cfg, nil, func(ctx context.Context, client *rpc.Client, p config.Provider) display.CompareResult {
		_, _, _ = client.BlockNumber(ctx)

		block, latency, err := client.GetBlock(ctx, blockNum)

		r := display.CompareResult{Provider: p.Name, Latency: latency, Error: err}
		if err == nil && block != nil {
			r.Hash = block.Hash
			r.Height, _ = rpc.ParseHexUint64(block.Number)
		}
		return r
	})

	hashGroups := make(map[string][]display.CompareResult)
	heightGroups := make(map[uint64][]display.CompareResult)
	for _, r := range results {
		if r.Error == nil {
			hashGroups[r.Hash] = append(hashGroups[r.Hash], r)
			heightGroups[r.Height] = append(heightGroups[r.Height], r)
		}
	}

	successCount := 0
	for _, r := range results {
		if r.Error == nil {
			successCount++
		}
	}

	hasHeightMismatch := len(heightGroups) > 1
	hasHashMismatch := len(hashGroups) > 1

	if jsonOut {
		blockArgCopy := blockArg
		successCountCopy := successCount
		totalCountCopy := len(results)
		hasHeightMismatchCopy := hasHeightMismatch
		hasHashMismatchCopy := hasHashMismatch

		reportData := report.Report{
			BlockArg:          &blockArgCopy,
			Timestamp:         time.Now(),
			Results:           make([]report.Entry, len(results)),
			HeightGroups:      make(map[uint64][]string),
			HashGroups:        make(map[string][]string),
			SuccessCount:      &successCountCopy,
			TotalCount:        &totalCountCopy,
			HasHeightMismatch: &hasHeightMismatchCopy,
			HasHashMismatch:   &hasHashMismatchCopy,
		}

		for i, r := range results {
			entry := report.Entry{Provider: r.Provider}
			if r.Error != nil {
				errStr := r.Error.Error()
				entry.Error = &errStr
			} else {
				hash := r.Hash
				height := r.Height
				entry.Hash = &hash
				entry.Height = &height
			}
			latency := report.MillisDuration(r.Latency)
			entry.LatencyMS = &latency
			reportData.Results[i] = entry
		}

		for height, results := range heightGroups {
			providers := make([]string, len(results))
			for i, r := range results {
				providers[i] = r.Provider
			}
			reportData.HeightGroups[height] = providers
		}

		for hash, results := range hashGroups {
			providers := make([]string, len(results))
			for i, r := range results {
				providers[i] = r.Provider
			}
			reportData.HashGroups[hash] = providers
		}

		filepath, err := report.WriteJSON(reportData, "compare")
		if err != nil {
			return fmt.Errorf("failed to write JSON report: %w", err)
		}
		fmt.Fprintf(os.Stderr, "JSON report written to: %s\n", filepath)
		return nil
	}

	formatter := display.NewCompareFormatter(results, successCount, heightGroups, hashGroups, hasHeightMismatch, hasHashMismatch)
	if err := formatter.Format(os.Stdout); err != nil {
		return fmt.Errorf("failed to display results: %w", err)
	}

	return nil
}
