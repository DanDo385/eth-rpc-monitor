// Package cli contains the command workflows for the eth-rpc-monitor CLI.
//
// Each Run* function (RunBlock, RunTest, RunSnapshot, RunMonitor) is the
// pure workflow logic for one subcommand, factored out of package main so it
// can be imported and tested independently of the cobra command layer in
// cmd/ethrpc. The Run* functions read configuration, drive the internal rpc /
// format / reportjson packages, and write directly to os.Stdout / os.Stderr
// exactly as the previous standalone binaries did.
package cli
