MIDI File Library
=================

This project contains my own implementation of a library for reading or writing
standard MIDI files (SMF) in go.  Other more-complete (and likely better)
libraries already exist; this one was written for my own interest and
education. For the time being, it only supports reading or writing SMFs.

Basic Usage
-----------

The following can be done to parse a ".mid" file:
```go
package main

import (
	"fmt"
	"github.com/yalue/midi"
	"os"
)

func main() {
	inputFile, e := os.Open("./my_file.mid")
	if e != nil {
		fmt.Printf("Failed opening file: %s\n", e)
		return
	}
	defer inputFile.Close()
	smf, e := midi.ParseSMFFile(inputFile)
	if e != nil {
		fmt.Printf("Failed parsing file: %s\n", e)
		return
	}
	fmt.Printf("Events in track 0:\n")
	track := smf.Tracks[0]
	for i, m := range track.Messages {
		fmt.Printf("Event %d. Time %d: %s\n", i, track.TimeDeltas[i], m)
	}
}
```

The `smf_tool` directory contains a command-line utility that may contain more
complete illustrations of this library's usage.

Internally, the tool uses the `MIDIMessage` interface to represent events in a
track. Type assertions can be used to convert a `MIDIMessage` to a specific
type from which information can be extracted. See
[godoc](https://godoc.org/github.com/yalue/midi) for more information.

