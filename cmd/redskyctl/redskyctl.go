package main

import (
	crypto_rand "crypto/rand"
	"encoding/binary"
	"math/rand"
	"os"
	"path/filepath"

	"github.com/gramLabs/redsky/pkg/redskyctl/cmd"
	"github.com/gramLabs/redsky/pkg/redskyctl/cmd/generate"
	"github.com/gramLabs/redsky/pkg/redskyctl/cmd/setup"
	"github.com/spf13/cobra"
)

func main() {
	// Seed the pseudo random number generator using the cryptographic random number generator
	// https://stackoverflow.com/a/54491783
	var b [8]byte
	_, err := crypto_rand.Read(b[:])
	if err != nil {
		panic(err)
	}
	rand.Seed(int64(binary.LittleEndian.Uint64(b[:])))

	// Determine which command to run
	var command *cobra.Command
	switch filepath.Base(os.Args[0]) {
	case setup.KustomizePluginKind:
		command = cmd.NewDefaultCommand(generate.NewGenerateCommand)
	default:
		command = cmd.NewDefaultRedskyctlCommand()
	}

	// Run the command
	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}
