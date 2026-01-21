package rpc

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

func ParseHexUint64(hex string) (uint64, error) {
	return strconv.ParseUint(strings.TrimPrefix(hex, "0x"), 16, 64)
}

func ParseHexBigInt(hex string) *big.Int {
	val := new(big.Int)
	val.SetString(strings.TrimPrefix(hex, "0x"), 16)
	return val
}

func FormatTimestamp(ts uint64) string {
	t := time.Unix(int64(ts), 0).UTC()
	ago := time.Since(t).Truncate(time.Second)
	return fmt.Sprintf("%s (%s ago)", t.Format("2006-01-02 15:04:05 UTC"), ago)
}

func FormatNumber(n uint64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

func FormatGwei(wei *big.Int) string {
	if wei == nil {
		return "—"
	}
	gwei := new(big.Float).Quo(new(big.Float).SetInt(wei), big.NewFloat(1e9))
	f, _ := gwei.Float64()
	return fmt.Sprintf("%.2f gwei", f)
}
