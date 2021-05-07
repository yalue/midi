// This defines a command-line utility for gathering information about
// instruments used by MIDI files.
package main

import (
	"flag"
	"fmt"
	"github.com/yalue/midi"
	"os"
	"path/filepath"
	"runtime"
)

// Keeps track of our accumulated event count for each instrument.
type instrumentStats struct {
	// A slice containing 128 entries: one value per MIDI instrument. Each
	// value will be set to the number of times that instrument was used in an
	// event.
	eventCounts [128]uint64
	// A slice containing 128 entries: one value per MIDI percussion
	// instrument event (basically, a count of each note played on channel 10)
	percussionEventCounts [128]uint64
}

// Dumps the total counts for each instrument to stdout.
func (s *instrumentStats) printInfo() {
	for i := 0; i < 128; i++ {
		fmt.Printf("Instrument %d: %d events.\n", i, s.eventCounts[i])
	}
	for i := 0; i < 128; i++ {
		fmt.Printf("Percussion instrument %d: %d events.\n", i,
			s.percussionEventCounts[i])
	}
}

// Adds the instrument-events for the named MIDI file to the running totals.
// Returns an error if one occurs.
func (s *instrumentStats) addFile(name string) error {
	f, e := os.Open(name)
	if e != nil {
		return fmt.Errorf("Failed opening %s: %w", name, e)
	}
	defer f.Close()
	smf, e := midi.ParseSMFFile(f)
	if e != nil {
		return fmt.Errorf("Failed parsing %s: %w", name, e)
	}
	var channelInstruments [16]uint8
	for _, track := range smf.Tracks {
		// For each track we'll reset the known instruments to 0. This may be
		// incorrect...
		for i := 0; i < 16; i++ {
			channelInstruments[i] = 0
		}
		for _, message := range track.Messages {
			// We only care about program-change and note-on events in order to
			// figure out the number of times each instrument is played.
			noteOn, isNoteOn := message.(*midi.NoteOnEvent)
			if isNoteOn {
				if noteOn.Velocity == 0 {
					// Note on with 0 velocity actually turns off the note;
					// don't count it.
					continue
				}
				// Percussion = anything in channel 10 (index 9)
				if noteOn.Channel == 9 {
					s.percussionEventCounts[noteOn.Note]++
				} else {
					s.eventCounts[channelInstruments[noteOn.Channel]]++
				}
				continue
			}

			// Update the instrument associated with the specified channel if
			// this is a program-change event.
			progChange, isProgChange := message.(*midi.ProgramChangeEvent)
			if isProgChange {
				channelInstruments[progChange.Channel] = progChange.Value
				continue
			}
		}
	}
	return nil
}

func run() int {
	var baseDir string
	flag.StringVar(&baseDir, "dir", "", "The directory to scan for .mid files")
	flag.Parse()
	if baseDir == "" {
		fmt.Println("A base directory must be specified." +
			"Run with -help for usage.")
		return 1
	}
	filenames, e := filepath.Glob(baseDir + "/*.mid")
	if e != nil {
		fmt.Printf("Failed looking up MIDI files in dir %s: %s\n", baseDir, e)
		return 1
	}
	if len(filenames) <= 0 {
		fmt.Printf("Didn't find any MIDI (.mid) files in dir %s.\n", baseDir)
		return 1
	}
	stats := &instrumentStats{}
	for i, name := range filenames {
		fmt.Printf("Scanning file %d/%d: %s\n", i+1, len(filenames), name)
		e = stats.addFile(name)
		if e != nil {
			fmt.Printf("Failed analyzing file %s: %s\n", name, e)
		}
		runtime.GC()
	}
	stats.printInfo()
	return 0
}

func main() {
	os.Exit(run())
}
