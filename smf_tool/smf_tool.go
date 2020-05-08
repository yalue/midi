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
	"strconv"
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

// Converts the given string to a number, and verifies that the number is
// between 0 and 15 (inclusive).
func stringToChannelNumber(s string) (uint8, error) {
	v, e := strconv.Atoi(s)
	if e != nil {
		return 0, fmt.Errorf("Couldn't convert %s to number: %s", s, e)
	}
	if (v < 0) || (v > 15) {
		return 0, fmt.Errorf("Invalid channel number: %d. "+
			"Channel numbers start at 0 in this tool (for now).", v)
	}
	return uint8(v), nil
}

// We'll use this interface to identify and modify events that are associated
// with a channel.
type ChannelMessage interface {
	midi.MIDIMessage
	GetChannel() uint8
	SetChannel(c uint8) error
}

// Modifies the SMFFile struct to reassign every event in one channel to happen
// in a different channel instead. I used this to fix a broken MIDI file that
// incorrectly put some non-percussion in channel 10. We'll use channel numbers
// starting from 0 here (probably should make that consistent later).
func reassignChannels(args string, smf *midi.SMFFile) error {
	channelStrings := strings.Split(args, ",")
	if len(channelStrings) != 2 {
		return fmt.Errorf("%s doesn't contain two channels numbers", args)
	}
	originalChannel, e := stringToChannelNumber(channelStrings[0])
	if e != nil {
		return fmt.Errorf("Bad original channel number: %s", e)
	}
	newChannel, e := stringToChannelNumber(channelStrings[1])
	if e != nil {
		return fmt.Errorf("Bad new channel number: %s", e)
	}
	totalCount := 0
	modifiedCount := 0
	for _, t := range smf.Tracks {
		for _, m := range t.Messages {
			totalCount++
			channelMessage, ok := m.(ChannelMessage)
			if !ok {
				continue
			}
			if channelMessage.GetChannel() != originalChannel {
				continue
			}
			// We've found a channel message that is associated with the old
			// channel, so reassign it to the new channel.
			e = channelMessage.SetChannel(newChannel)
			if e != nil {
				return fmt.Errorf("Failed setting channel on %s: %s", m, e)
			}
			modifiedCount++
		}
	}
	fmt.Printf("Reassigned %d/%d events from channel %d to %d.\n", modifiedCount,
		totalCount, originalChannel, newChannel)
	return nil
}

// Scales the velocity of every event in the indicated track.
func rescaleVelocity(scale float64, track int, smf *midi.SMFFile) error {
	if (scale < 0) || (scale >= 1) {
		return fmt.Errorf("Velocity scale must be between 0 and 1. Got %f",
			scale)
	}
	t, e := getNumberedTrack(track, smf)
	if e != nil {
		return e
	}
	modifiedCount := 0
	for _, m := range t.Messages {
		noteOn, ok := m.(*midi.NoteOnEvent)
		if !ok {
			continue
		}
		newVelocity := uint8(float64(noteOn.Velocity) * scale)
		if newVelocity > 127 {
			newVelocity = 127
		}
		noteOn.Velocity = newVelocity
		modifiedCount++
	}
	fmt.Printf("Updated the velocity of %d note-on events in track %d\n",
		modifiedCount, track)
	return nil
}

// Sets the time delta of the event at the given track and position.
func adjustTimeDelta(newTimeDelta, track, position int,
	smf *midi.SMFFile) error {
	if newTimeDelta > 0x0fffffff {
		return fmt.Errorf("The time delta of %d exceeds the limit of %d",
			newTimeDelta, 0x0fffffff)
	}
	t, e := getNumberedTrack(track, smf)
	if e != nil {
		return e
	}
	index := position - 1
	if (index < 0) || (index >= len(t.TimeDeltas)) {
		return fmt.Errorf("Invalid track event number for delta-time "+
			"adjustment: %d", position)
	}
	t.TimeDeltas[index] = uint32(newTimeDelta)
	return nil
}

func deleteSMFEvent(track, position int, smf *midi.SMFFile) error {
	t, e := getNumberedTrack(track, smf)
	if e != nil {
		return e
	}
	index := position - 1
	if (index < 0) || (index >= len(t.Messages)) {
		return fmt.Errorf("Invalid event number for event to delete: %d",
			position)
	}
	// Shift all of the events past the deleted events up one position, and
	// shorten the slices by one.
	copy(t.TimeDeltas[index:], t.TimeDeltas[index+1:])
	t.TimeDeltas = t.TimeDeltas[0 : len(t.TimeDeltas)-1]
	copy(t.Messages[index:], t.Messages[index+1:])
	t.Messages = t.Messages[0 : len(t.Messages)-1]
	return nil
}

// Looks through the SMF file and computes the longest-running track, in ticks.
// Returns the number of ticks in this track.
func getLongestTrackTicks(smf *midi.SMFFile) uint32 {
	toReturn := uint32(0)
	for _, t := range smf.Tracks {
		current := uint32(0)
		for _, d := range t.TimeDeltas {
			current += d
		}
		if current > toReturn {
			toReturn = current
		}
	}
	return toReturn
}

// Adds an additional track with some more percussion to the SMF file. Attempts
// to make the new track's tempo match the tempo specified in the file header.
func addExtraBeats(smf *midi.SMFFile) error {
	ticksToGenerate := getLongestTrackTicks(smf)
	// We'll make this twice as fast as the MIDI itself.
	ticksPerBeat := uint32(smf.Division.TicksPerQuarterNote()) / 2
	if ticksPerBeat == 0 {
		return fmt.Errorf("Unsupported: The file doesn't specify ticks per " +
			"beat")
	}
	beatsToGenerate := ticksToGenerate / ticksPerBeat
	// For each beat we'll generate 1 note on event and one note-off event,
	// plus one end-of-track event.
	eventCount := beatsToGenerate*2 + 1
	messages := make([]midi.MIDIMessage, 0, eventCount)
	timeDeltas := make([]uint32, 0, eventCount)
	// This specifies the pattern of notes to play, apart from delta times.
	onEvents := []midi.MIDIMessage{
		&midi.NoteOnEvent{
			// We'll rely on channel 9 being reserved for percussion, as is the
			// case for general MIDI.
			Channel: 9,
			// This is the bass drum "note" for general MIDI percussion
			Note: 36,
			// Make this pretty loud
			Velocity: 120,
		},
		&midi.NoteOnEvent{
			Channel: 9,
			// Closed hi-hat
			Note: 42,
			// Slightly quieter
			Velocity: 80,
		},
		&midi.NoteOnEvent{
			Channel: 9,
			// Electric snare
			Note:     40,
			Velocity: 100,
		},
		&midi.NoteOnEvent{
			Channel:  9,
			Note:     42,
			Velocity: 80,
		},
	}
	offEvents := make([]midi.MIDIMessage, len(onEvents))
	// We'll use note-on events with velocity 0 for the note-off events.
	for i := range onEvents {
		onEvent := onEvents[i].(*midi.NoteOnEvent)
		offEvent := *onEvent
		offEvent.Velocity = 0
		offEvents[i] = &offEvent
	}

	// Populate the new track's times and events slices.
	for i := 0; i < int(beatsToGenerate); i++ {
		// Note-on events will always have a time delta of 0--they'll happen at
		// the same time as the preceding note-off event.
		timeDeltas = append(timeDeltas, 0)
		messages = append(messages, onEvents[i%len(onEvents)])
		timeDeltas = append(timeDeltas, ticksPerBeat)
		messages = append(messages, offEvents[i%len(offEvents)])
	}
	// Don't forget the end-of-track messages
	timeDeltas = append(timeDeltas, 0)
	messages = append(messages, midi.EndOfTrackMetaEvent(0))

	// Finally, create the new track and append it to the SMF's tracks.
	newTrack := &midi.SMFTrack{
		Messages:   messages,
		TimeDeltas: timeDeltas,
	}
	smf.Tracks = append(smf.Tracks, newTrack)
	fmt.Printf("Appended track %d, with %d events.\n", len(smf.Tracks),
		len(messages))
	return nil
}

func run() int {
	var filename, outputFilename string
	var dumpEvents bool
	var track, position int
	var reassignChannel string
	var newEventHex string
	var deleteEvent bool
	var newTimeDelta int
	var scaleVelocity float64
	var bootsAndCats bool
	flag.StringVar(&filename, "input_file", "", "The .mid file to open.")
	flag.StringVar(&outputFilename, "output_file", "", "The name of the .mid "+
		"file to create.")
	flag.BoolVar(&dumpEvents, "dump_events", false, "If set, print a list of "+
		"all events in the file to stdout.")
	flag.IntVar(&track, "track", -1, "The track to modify.")
	flag.IntVar(&position, "position", -1, "The position in the track to "+
		"modify. If inserting a message, it will be inserted after this "+
		"position. 0 = insert at the first position.")
	flag.IntVar(&newTimeDelta, "new_time_delta", -1, "Set the time delta of "+
		"the event specified by -position and -track to this value.  This "+
		"will be applied before -new_event.")
	flag.StringVar(&newEventHex, "new_event", "", "Provide a hex string of "+
		"bytes here, containing a delta time followed by a MIDI message to "+
		"insert at the given position. Must be a valid SMF event, and not "+
		"use running status.")
	flag.StringVar(&reassignChannel, "reassign_channel", "", "If provided, "+
		"this must be a comma-separated list of two integers indicating "+
		"channel numbers. Any events in the channel indicated by the first "+
		"number will be modified to happen in the second channel's number "+
		"instead. Uses channel numbers starting from 0.")
	flag.Float64Var(&scaleVelocity, "scale_velocity", -1, "If provided, "+
		"this must be a value between 0.0 and 1.0. The velocity of every "+
		"note-on event in the selected track will be scaled by this amount.")
	flag.BoolVar(&bootsAndCats, "boots_and_cats", false, "If set, this adds "+
		"an extra track to the MIDI file, for added rhythmic emphasis!")
	flag.BoolVar(&deleteEvent, "delete_event", false, "If set, delete the "+
		"event at the specified track and position. No other modifications"+
		"can be made if this is specified.")
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

	if deleteEvent {
		e = deleteSMFEvent(track, position, smf)
		if e != nil {
			fmt.Printf("Failed deleting event: %s\n", e)
			return 1
		}
	}

	// Adjust time deltas first, if requested.
	if newTimeDelta >= 0 {
		if deleteEvent {
			fmt.Printf("Can't adjust time delta after deleting an event.\n")
			return 1
		}
		e = adjustTimeDelta(newTimeDelta, track, position, smf)
		if e != nil {
			fmt.Printf("Failed adjusting time delta: %s\n", e)
			return 1
		}
	}

	// Insert a new message if one was specified.
	if newEventHex != "" {
		if deleteEvent {
			fmt.Printf("Can't add new event after deleting an event.\n")
		}
		e = insertNewEvent(newEventHex, track, position, smf)
		if e != nil {
			fmt.Printf("Failed inserting new event: %s\n", e)
			return 1
		}
	}

	// Next, reassign channel numbers if requested.
	if reassignChannel != "" {
		e = reassignChannels(reassignChannel, smf)
		if e != nil {
			fmt.Printf("Failed reassigning channel numbers: %s\n", e)
			return 1
		}
	}

	if (scaleVelocity >= 0) && (scaleVelocity <= 1.0) {
		e = rescaleVelocity(scaleVelocity, track, smf)
		if e != nil {
			fmt.Printf("Failed scaling track velocity: %s\n", e)
			return 1
		}
	}

	if bootsAndCats {
		e = addExtraBeats(smf)
		if e != nil {
			fmt.Printf("Failed adding extra track: %s\n", e)
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
