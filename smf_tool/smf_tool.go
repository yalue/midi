// This defines a command-line utility for viewing or manipulating standard
// MIDI files (SMF, usually with a ".mid" extension).
package main

import (
	"flag"
	"fmt"
	"github.com/yalue/midi"
	"os"
)

func run() int {
	var filename string
	var dumpEvents bool
	flag.StringVar(&filename, "input_file", "", "The .mid file to open.")
	flag.BoolVar(&dumpEvents, "dump_events", false, "If set, print a list of "+
		"all events in the file to stdout.")
	flag.Parse()
	if filename == "" {
		fmt.Printf("Invalid arguments. Run with -help for more information.\n")
		return 1
	}
	inputFile, e := os.Open(filename)
	if e != nil {
		fmt.Printf("Couldn't open %s: %s\n", filename, e)
		return 1
	}
	defer inputFile.Close()
	smf, e := midi.ParseSMFFile(inputFile)
	if e != nil {
		fmt.Printf("Couldn't parse %s: %s\n", filename, e)
		return 1
	}
	fmt.Printf("Parsed %s OK. Contains %d tracks. Time division: %s.\n",
		filename, len(smf.Tracks), smf.Division)
	if dumpEvents {
		for i, t := range smf.Tracks {
			fmt.Printf("Track %d (%d events):\n", i, len(t.Messages))
			for j, m := range t.Messages {
				fmt.Printf("  %d. Time %d: %s\n", j, t.TimeDeltas[j], m)
			}
		}
	}
	return 0
}

func main() {
	os.Exit(run())
}
