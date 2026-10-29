package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/nd4y/atblse/core"
)

func main() {
	dir := flag.String("data-dir", "data", "server data directory")
	uuid := flag.String("uuid", "", "official catalog UUID")
	file := flag.String("file", "", "native .bin file")
	flag.Parse()
	if err := run(*dir, *uuid, *file); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Catalog file imported; no device command sent.")
}

func run(dir, uuid, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 600009))
	if err != nil {
		return err
	}
	return core.ImportLocalScript(dir, uuid, data)
}
