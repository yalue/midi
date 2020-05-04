// This defines a command-line utility for viewing or manipulating standard
// MIDI files (SMF, usually with a ".mid" extension).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/yalue/midi"
	"os"
	"regexp"
	"strings"
)

// Returns the value of a lower-case hex char
func hexCharToValue(b byte) byte {
	if (b >= '0') && (b <= '9') {
		return b - '0'
	}
	if (b >= 'a') && (b <= 'f') {
		return b - 'a' + 10
	}
	panic("Bad lowercase hex char.")
	return 0
}

// Converts the string s to bytes. The string may only contain hex chars and
// whitespace.
func hexStringToBytes(s string) ([]byte, error) {
	// Strip all whitespace out of s.
	s = regexp.MustCompile(`\s`).ReplaceAllString(s, "")
	s = strings.ToLower(s)
	// Ensure s is an even number of hex characters.
	ok, e := regexp.MatchString(`^([a-f0-9]{2})*$`, s)
	if e != nil {
		return nil, fmt.Errorf("Error validating hex string: %s", e)
	}
	if !ok {
		return nil, fmt.Errorf("Invalid hex bytes string")
	}
	textBytes := []byte(s)
	toReturn := make([]byte, len(textBytes)/2)
	for i := range toReturn {
		a := hexCharToValue(textBytes[i*2])
		b := hexCharToValue(textBytes[i*2+1])
		toReturn[i] = byte(b) | (a << 4)
	}
	return toReturn, nil
}

// Takes a track number (with 1 being the first track), and returns a pointer
// to the track's data in the given SMFFile.
func getNumberedTrack(track int, smf *midi.SMFFile) (*midi.SMFTrack, error) {
	if track <= 0 {
		return nil, fmt.Errorf("Invalid track number: %d. Note that track "+
			"numbering starts at 1, rather than 0.", track)
	}
	if (track - 1) >= len(smf.Tracks) {
		return nil, fmt.Errorf("Invalid track number: %d. The file only "+
			"contains %d tracks.", track, len(smf.Tracks))
	}
	return smf.Tracks[track-1], nil
}

// Modifies the given SMF file to insert a new event, encoded as a hex string,
// after the event at the given position in the given track.
func insertNewEvent(hexData string, track, position int,
	smf *midi.SMFFile) error {
	t, e := getNumberedTrack(track, smf)
	if e != nil {
		return e
	}
	if (position < 0) || (position >= len(t.Messages)) {
		return fmt.Errorf("Invalid track position: %d", position)
	}
	data, e := hexStringToBytes(hexData)
	if e != nil {
		return fmt.Errorf("Invalid new event data: %s", e)
	}
	r := bytes.NewReader(data)
	deltaTime, e := midi.ReadVariableInt(r)
	if e != nil {
		return fmt.Errorf("Couldn't read new event's delta time: %s", e)
	}
	fmt.Printf("New event delta time: %d\n", deltaTime)
	runningStatus := byte(0)
	event, e := midi.ReadSMFMessage(r, &runningStatus)
	if e != nil {
		return fmt.Errorf("Couldn't parse new event: %s", e)
	}
	fmt.Printf("Inserting new event: %s\n", event)
	newTimes := make([]uint32, len(t.TimeDeltas)+1)
	newMessages := make([]midi.MIDIMessage, len(t.Messages)+1)
	// Copy the events and times before the new event.
	copy(newTimes[0:position+1], t.TimeDeltas[0:position+1])
	copy(newMessages[0:position+1], t.Messages[0:position+1])
	// Insert the new event
	newTimes[position] = deltaTime
	newMessages[position] = event
	// Copy the events and times after the new event.
	copy(newTimes[position+1:len(newTimes)],
		t.TimeDeltas[position:len(t.TimeDeltas)])
	copy(newMessages[position+1:len(newMessages)],
		t.Messages[position:len(t.Messages)])
	// Modify the SMFFile struct to point to the modified slices
	t.TimeDeltas = newTimes
	t.Messages = newMessages
	return nil
}

func run() int {
	var filename, outputFilename string
	var dumpEvents bool
	var track, position int
	var newEventHex string
	flag.StringVar(&filename, "input_file", "", "The .mid file to open.")
	flag.StringVar(&outputFilename, "output_file", "", "The name of the .mid "+
		"file to create.")
	flag.BoolVar(&dumpEvents, "dump_events", false, "If set, print a list of "+
		"all events in the file to stdout.")
	flag.IntVar(&track, "track", -1, "The track to modify.")
	flag.IntVar(&position, "position", -1, "The position in the track to "+
		"modify. If inserting a message, it will be inserted after this "+
		"position. 0 = insert at the first position.")
	flag.StringVar(&newEventHex, "new_event", "", "Provide a hex string of "+
		"bytes here, containing a delta time followed by a MIDI message to "+
		"insert at the given position. Must be a valid SMF event, and not "+
		"use running status.")
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
	smf, e := midi.ParseSMFFile(inputFile)
	// We'll close the input file here in case the output file overwrites it.
	inputFile.Close()
	if e != nil {
		fmt.Printf("Couldn't parse %s: %s\n", filename, e)
		return 1
	}
	fmt.Printf("Parsed %s OK. Contains %d tracks. Time division: %s.\n",
		filename, len(smf.Tracks), smf.Division)

	// First, insert a new message if one was specified.
	if newEventHex != "" {
		if outputFilename == "" {
			fmt.Printf("Error: can't insert a new event, an output file " +
				"name must be provided.\n")
			return 1
		}
		e = insertNewEvent(newEventHex, track, position, smf)
		if e != nil {
			fmt.Printf("Failed inserting new event: %s\n", e)
			return 1
		}
	}

	// Dump the events after any modifications.
	if dumpEvents {
		for i, t := range smf.Tracks {
			fmt.Printf("Track %d (%d events):\n", i+1, len(t.Messages))
			for j, m := range t.Messages {
				fmt.Printf("  %d. Time %d: %s\n", j+1, t.TimeDeltas[j], m)
			}
		}
	}

	// Finally, save the output file if one was specified.
	if outputFilename != "" {
		f, e := os.Create(outputFilename)
		if e != nil {
			fmt.Printf("Error creating output file %s: %s\n", outputFilename,
				e)
			return 1
		}
		defer f.Close()
		e = smf.WriteToFile(f)
		if e != nil {
			fmt.Printf("Error writing SMF file: %s\n", e)
			return 1
		}
		fmt.Printf("%s saved OK.\n", outputFilename)
	}
	return 0
}

func main() {
	os.Exit(run())
}
