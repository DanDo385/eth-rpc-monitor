// internal/env/env.go
package env

import (
	"os"
	"strings"
)

// Load reads environment variables from a .env file in the current directory
// and sets them using os.Setenv. If the .env file doesn't exist, it silently
// returns without error.
func Load() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return // .env file not found, skip
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			os.Setenv(strings.TrimSpace(parts[0]), strings.Trim(strings.TrimSpace(parts[1]), `"'`))
		}
	}
}
