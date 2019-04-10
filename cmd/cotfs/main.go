package main

import (
	"flag"
	"fmt"
	"github.com/cfagiani/cotfs/internal/app/cotfs"
	"log"
	"os"
	"path/filepath"
)

var progName = filepath.Base(os.Args[0])

func main() {
	log.SetFlags(0)
	log.SetPrefix(progName + ": ")

	flag.Usage = usage
	flag.Parse()

	if flag.NArg() != 2 {
		usage()
		os.Exit(2)
	}
	path := flag.Arg(0)
	mountpoint := flag.Arg(1)
	if err := cotfs.Mount(path, mountpoint); err != nil {
		log.Fatal(err)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage of %s:\n", progName)
	fmt.Fprintf(os.Stderr, "  %s <metadataDir> <mountPoint>\n", progName)
	flag.PrintDefaults()
}
