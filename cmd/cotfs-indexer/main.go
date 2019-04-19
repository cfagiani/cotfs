package main

import (
	"flag"
	"fmt"
	"github.com/cfagiani/cotfs/internal/app/indexer"
	"log"
	"os"
	"path/filepath"
	"sync"
)

var progName = filepath.Base(os.Args[0])

type dirFlag []string

func main() {
	log.SetFlags(0)
	log.SetPrefix(progName + ": ")

	var scanDirectories dirFlag
	flag.Var(&scanDirectories, "scanDir", "Directory to scan for existing files. Can be repeated.")

	flag.Usage = usage
	flag.Parse()

	if scanDirectories == nil || len(scanDirectories) == 0 || flag.NArg() != 1 {
		usage()
		os.Exit(2)
	}
	metadataPath := flag.Arg(0)

	var wg sync.WaitGroup
	wg.Add(len(scanDirectories))
	for _, dir := range scanDirectories {
		go func() {
			err := indexer.IndexPath(dir, metadataPath)
			if err != nil {
				fmt.Printf("could not index directory: %v", err)
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage of %s:\n", progName)
	fmt.Fprintf(os.Stderr, "  %s <metadataDir>\n", progName)
	flag.PrintDefaults()
}

func (i *dirFlag) String() string {
	var content = ""
	for _, val := range *i {
		content += fmt.Sprint(val)
	}
	return content
}

func (i *dirFlag) Set(value string) error {
	*i = append(*i, value)
	return nil
}
