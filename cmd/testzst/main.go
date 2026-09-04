package main

import (
	"flag"
	"io"
	"os"

	"github.com/sonicfromnewyoke/mithril/pkg/mlog"
	"github.com/sonicfromnewyoke/mithril/pkg/snapshot"
	"github.com/klauspost/compress/zstd"
)

func main() {
	zstdFilename := flag.String("zstd-file", "", "Path to zstd compressed file")
	concurrency := flag.Int("zstd-decoder-concurrency", 1, "Number of decoder goroutines")

	flag.Parse()

	file, err := os.Open(*zstdFilename)
	if err != nil {
		mlog.Log.Errorf("opening zstdFilename=%s: %v", *zstdFilename, err)
		os.Exit(1)
	}
	bmr, err := snapshot.NewBufMonReaderFromFile(file)
	if err != nil {
		mlog.Log.Errorf("making BufMonReader: %v", err)
		os.Exit(1)
	}
	zstdReader, err := zstd.NewReader(bmr, zstd.WithDecoderConcurrency(*concurrency))
	if err != nil {
		mlog.Log.Errorf("zstd.NewReader: %v", err)
		os.Exit(1)
	}

	_, err = zstdReader.WriteTo(io.Discard)
	if err != nil {
		mlog.Log.Errorf("WriteTo: %v", err)
		os.Exit(1)
	}
}
