package main

import (
	"bytes"
	"fmt"
	"log"
	"os"

	"github.com/mitchellh/cli"
	"github.com/stevvooe/continuity"
)

type initCommand struct {
}

func initCommandFactory() (cli.Command, error) {
	return &initCommand{}, nil
}

func (cmd *initCommand) Help() string {
	return "Usage: init <directory>"
}

func (cmd *initCommand) Synopsis() string {
	return "Initialize a continuity filesystem from directory"
}

func (cmd *initCommand) Run(args []string) int {
	if len(args) == 0 {
		return cli.RunResultHelp
	}

	options := continuity.ContextOptions{
		Digester: blobStore,
	}

	// Create digester using blob store
	ctx, err := continuity.NewContextWithOptions(args[0], options)
	if err != nil {
		log.Printf("error creating path context: %v", err)
		return 1
	}

	m, err := continuity.BuildManifest(ctx)
	if err != nil {
		log.Printf("error generating manifest: %v", err)
		return 1
	}

	p, err := continuity.Marshal(m)
	if err != nil {
		log.Printf("error marshaling manifest: %v", err)
		return 1
	}

	dgst, err := blobStore.Digest(bytes.NewReader(p))
	if err != nil {
		log.Printf("error getting digest of manifest: %v", err)
		return 1
	}

	fmt.Fprintf(os.Stdout, "%s\n", dgst)

	return 0
}
