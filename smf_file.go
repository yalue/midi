package midi

// This file contains code used for reading .mid SMF-format files.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// This corresponds to the division field of the MThd chunk.
type TimeDivision uint16

// Returns the number of ticks per quarter note, or 0 if the time division
// doesn't specify a number of ticks per quarter note.
func (d TimeDivision) TicksPerQuarterNote() uint16 {
	if (d & 0x8000) != 0 {
		return 0
	}
	return uint16(d)
}

// Returns the SMPTE time code (indicating the frames per second) followed by
// the number of MIDI ticks per frame, in that order. Returns 0, 0 if the
// TimeDivision value specifies the number of ticks per quarter note instead.
func (d TimeDivision) SMPTETimeCode() (uint8, uint8) {
	if (d & 0x8000) == 0 {
		return 0, 0
	}
	// Since the top bit is set, the frames per second is specified as a 2's
	// complement negative 8-bit integer. We'll convert it to a positive 8-bit
	// integer.
	fps := uint8(-int8(d >> 8))
	// The ticks per frame isn't negative, and is in the latter group of 8
	// bits.
	ticksPerFrame := uint8(d & 0xff)
	return fps, ticksPerFrame
}

func (d TimeDivision) String() string {
	if (d & 0x7fff) == 0 {
		return fmt.Sprintf("Invalid TimeDivision value: 0x%04x", uint16(d))
	}
	qnTicks := d.TicksPerQuarterNote()
	if qnTicks != 0 {
		return fmt.Sprintf("%d ticks per quarter note", qnTicks)
	}
	fps, ticksPerFrame := d.SMPTETimeCode()
	return fmt.Sprintf("%d frames per second, %d ticks per frame", fps,
		ticksPerFrame)
}

// Specifies the format used by the SMF file header.
type SMFHeader struct {
	// This must be 'MThd'
	ChunkType [4]byte
	// This must be 6
	ChunkSize uint32
	// This must be 0 or 1 (we don't support type-2 files for now). Type 1 can
	// contain multiple tracks, type 0 can only contain one track.
	Format uint16
	// The number of tracks in the file. Must be 1 if Format is 0.
	TrackCount uint16
	// Specifies what the delta-times mean in this file.
	Division TimeDivision
}

func (h *SMFHeader) String() string {
	return fmt.Sprintf("Format %d, with %d track(s), %s", h.Format,
		h.TrackCount, h.Division.String())
}

// This holds the content of a single MIDI track chunk.
type SMFTrack struct {
	// The list of MIDI messages in this track, in the order they appear.
	Messages []MIDIMessage
	// The time deltas for each MIDI message. Has the same length as the
	// Messages slice; TimeDeltas[i] is the time delta for Messages[i].
	TimeDeltas []uint32
}

// Writes the given track to the given output file.
func (t *SMFTrack) WriteToFile(file io.Writer) error {
	if len(t.Messages) != len(t.TimeDeltas) {
		return fmt.Errorf("Bad track: has %d messages, but %d times",
			len(t.Messages), len(t.TimeDeltas))
	}
	// The chunk size needs to go in the header, so we'll just dump the chunk's
	// data into memory first.
	chunkContent := &bytes.Buffer{}
	var e error
	var messageBytes []byte
	runningStatus := byte(0)
	for i := range t.TimeDeltas {
		e = WriteVariableInt(chunkContent, t.TimeDeltas[i])
		if e != nil {
			return fmt.Errorf("Couldn't write time delta for event %d: %s", i,
				e)
		}
		messageBytes, e = t.Messages[i].SMFData(&runningStatus)
		if e != nil {
			return fmt.Errorf("Couldn't get bytes for event %d: %s", i, e)
		}
		_, e = chunkContent.Write(messageBytes)
		if e != nil {
			return fmt.Errorf("Couldn't write message for event %d: %s", i, e)
		}
	}
	chunkType := [4]byte{'M', 'T', 'r', 'k'}
	e = binary.Write(file, binary.BigEndian, chunkType)
	if e != nil {
		return fmt.Errorf("Failed writing chunk type: %s", e)
	}
	chunkSize := uint32(chunkContent.Len())
	e = binary.Write(file, binary.BigEndian, &chunkSize)
	if e != nil {
		return fmt.Errorf("Failed writing chunk size: %s", e)
	}
	_, e = file.Write(chunkContent.Bytes())
	if e != nil {
		return fmt.Errorf("Failed writing chunk content: %s", e)
	}
	return nil
}

// Parses and returns an SMF track, assuming the given reader is at the start
// of a track.
func parseSMFTrack(file io.Reader) (*SMFTrack, error) {
	chunkType := make([]byte, 4)
	e := binary.Read(file, binary.BigEndian, chunkType)
	if e != nil {
		return nil, fmt.Errorf("Failed reading track's chunk type: %s", e)
	}
	if string(chunkType) != "MTrk" {
		return nil, fmt.Errorf("Bad chunk type for track: %q",
			string(chunkType))
	}
	var length uint32
	e = binary.Read(file, binary.BigEndian, &length)
	if e != nil {
		return nil, fmt.Errorf("Failed reading track's length: %s", e)
	}
	// We'll just guess for now that the track will require approximately 3
	// bytes per event.
	messages := make([]MIDIMessage, 0, length/3)
	timeDeltas := make([]uint32, 0, length/3)
	// We'll use a limitedReader to ensure that a track's data fits within its
	// stated length.
	limitedReader := &io.LimitedReader{
		R: file,
		N: int64(length),
	}
	var timeDelta uint32
	var message MIDIMessage
	eventCount := 0
	runningStatus := byte(0)
	for {
		timeDelta, e = ReadVariableInt(limitedReader)
		if e != nil {
			// We know we've properly read the full track if we encounter EOF
			// when attempting to start reading a new event.
			if e == io.EOF {
				break
			}
			return nil, fmt.Errorf("Failed reading time delta for event "+
				"%d: %s", eventCount, e)
		}
		timeDeltas = append(timeDeltas, timeDelta)
		message, e = ReadSMFMessage(limitedReader, &runningStatus)
		if e != nil {
			return nil, fmt.Errorf("Failed reading MIDI message for event "+
				"%d: %s", eventCount, e)
		}
		messages = append(messages, message)
	}
	return &SMFTrack{
		TimeDeltas: timeDeltas,
		Messages:   messages,
	}, nil
}

// Tracks an entire MIDI file, consisting of one or more tracks and timing
// information.
type SMFFile struct {
	// TODO: Replace TimeDivision with something more human-usable here; we can
	// format it when writing the file.
	Division TimeDivision
	Tracks   []*SMFTrack
}

// Parses the given SMF file, returning an initialized SMFFile struct, or an
// error if the file was invalid.
func ParseSMFFile(file io.Reader) (*SMFFile, error) {
	var toReturn SMFFile
	var header SMFHeader
	e := binary.Read(file, binary.BigEndian, &header)
	if e != nil {
		return nil, fmt.Errorf("Failed parsing SMF header: %s", e)
	}
	toReturn.Division = header.Division
	toReturn.Tracks = make([]*SMFTrack, header.TrackCount)
	for i := 0; i < len(toReturn.Tracks); i++ {
		toReturn.Tracks[i], e = parseSMFTrack(file)
		if e != nil {
			return nil, fmt.Errorf("Failed parsing SMF track %d: %s", i, e)
		}
	}
	return &toReturn, nil
}

// Writes the given SMF file to an output file. Uses running status when
// writing the output.
func (f *SMFFile) WriteToFile(file io.Writer) error {
	var header SMFHeader
	header.ChunkType = [4]byte{'M', 'T', 'h', 'd'}
	header.ChunkSize = 6
	if len(f.Tracks) > 0xffff {
		return fmt.Errorf("Have too many tracks (%d), limited to %d",
			len(f.Tracks), 0xffff)
	}
	header.TrackCount = uint16(len(f.Tracks))
	if len(f.Tracks) == 1 {
		header.Format = 0
	} else {
		header.Format = 1
	}
	header.Division = f.Division
	e := binary.Write(file, binary.BigEndian, &header)
	if e != nil {
		return fmt.Errorf("Failed writing SMF header: %s", e)
	}
	for i, t := range f.Tracks {
		e = t.WriteToFile(file)
		if e != nil {
			return fmt.Errorf("Failed writing SMF track %d: %s", i, e)
		}
	}
	return nil
}
