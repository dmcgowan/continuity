package main

import (
	"log"
	"os"
	"path/filepath"

	_ "crypto/sha256"

	"github.com/mitchellh/cli"
)

var (
	blobStore *BlobStore
	mountDir  string
)

func init() {
	continuityPath := os.Getenv("CONTINUITY_PATH")
	if continuityPath == "" {
		continuityPath = filepath.Join(os.Getenv("HOME"), ".local", "continuity")
	}

	var err error
	blobStore, err = NewBlobStore(filepath.Join(continuityPath, "blobs"))
	if err != nil {
		panic(err)
	}

	mountDir = filepath.Join(continuityPath, "mounts")
	if err := os.MkdirAll(mountDir, 0755); err != nil {
		panic(err)
	}
}

func main() {
	c := cli.NewCLI("contfs", "0.0.1")

	c.Args = os.Args[1:]
	c.Commands = map[string]cli.CommandFactory{
		"init":  initCommandFactory,
		"mount": mountCommandFactory,
	}

	exitStatus, err := c.Run()
	if err != nil {
		log.Println(err)
	}

	os.Exit(exitStatus)
}
