// This package defines a library for reading and writing MIDI or devices. The
// midi_tool directory contains a command-line interface that exposes most of
// the library's features.
package midi

import (
	"bytes"
	"fmt"
	"io"
)

// Reads and returns the next byte from r.
func readByte(r io.Reader) (uint8, error) {
	tmp := []uint8{0}
	_, e := r.Read(tmp)
	return tmp[0], e
}

// Reads a MIDI-format variable int (up to 0x0fffffff). Returns an error if one
// occurs, including if the int being read is larger than 0x0fffffff. Will
// return an io.EOF error if and only if the io.EOF occurs when attempting to
// read the first byte of the integer.
func ReadVariableInt(r io.Reader) (uint32, error) {
	toReturn := uint32(0)
	for i := 0; i < 4; i++ {
		b, e := readByte(r)
		if e != nil {
			if i == 0 {
				// Make sure io.EOF gets propagated up here.
				return 0, e
			}
			return 0, fmt.Errorf("Failed reading full integer: %s", e)
		}
		toReturn |= uint32(b & 0x7f)
		if (b & 0x80) == 0 {
			break
		}
		toReturn = toReturn << 7
		if i == 3 {
			return 0, fmt.Errorf("Invalid variable-length integer: highest " +
				"bit not clear on byte 4")
		}
	}
	return toReturn, nil
}

// Writes a MIDI-format variable int (up to 0x0fffffff) to the given output
// stream. Returns an error if one occurs, including if the integer is invalid.
func WriteVariableInt(w io.Writer, n uint32) error {
	var e error
	if n > 0x0fffffff {
		return fmt.Errorf("Integer 0x%08x is too large for a MIDI int", n)
	}
	// Special simplifying case: just write a 0 if the number was 0.
	if n == 0 {
		_, e = w.Write([]byte{0})
	}
	// Break the number up into 7-bit chunks
	toWrite := make([]byte, 0, 4)
	for n != 0 {
		b := uint8(n & 0x7f)
		toWrite = append(toWrite, b)
		n = n >> 7
	}
	// Now we'll need to generate the actual slice to write: we need to reverse
	// the bytes in toWrite, and set the top bit on all but the last byte.
	reversed := make([]byte, len(toWrite))
	for i := len(toWrite) - 1; i >= 0; i-- {
		b := toWrite[i]
		if i != 0 {
			b |= 0x80
		}
		reversed[len(reversed)-i-1] = b
	}
	_, e = w.Write(reversed)
	return e
}

// A basic interface that all MIDI messages support.
type MIDIMessage interface {
	// A string representation of the event.
	String() string
	// Returns the underlying bytes for this message, as it would be written to
	// an SMF file. Requires a running status byte, which will be updated if
	// necessary.
	SMFData(runningStatus *byte) ([]byte, error)
}

// Holds a sysex-type message. Implements the MIDIMessage interface.
type SystemExclusiveMessage struct {
	// Holds all bytes in the message, not including the leading F0 or trailing
	// F7.
	DataBytes []byte
}

func (m *SystemExclusiveMessage) String() string {
	return fmt.Sprintf("System exclusive message. %d bytes: % x.",
		len(m.DataBytes), m.DataBytes)
}

// Formats the system-exclusive message
func (m *SystemExclusiveMessage) SMFData(runningStatus *byte) ([]byte, error) {
	*runningStatus = 0
	// Make sure we are able to fit the length of the data, plus one byte for
	// the trailing 0xf7, into a 32-bit variable length MIDI integer.
	if (len(m.DataBytes) + 1) > 0x0fffffff {
		return nil, fmt.Errorf("System exclusive message too big for SMF " +
			"event")
	}
	var toReturn bytes.Buffer
	toReturn.WriteByte(0xf0)
	e := WriteVariableInt(&toReturn, uint32(len(m.DataBytes)+1))
	if e != nil {
		return nil, fmt.Errorf("Failed formatting sysex message length: %s", e)
	}
	toReturn.Write(m.DataBytes)
	toReturn.WriteByte(0xf7)
	return toReturn.Bytes(), nil
}

// Reads the next system exclusive message from the given input stream. The
// first byte (F0 or F7) must have already been read, and must be passed in as
// the firstByte argument.
func parseSystemExclusiveMessage(r io.Reader, firstByte byte) (MIDIMessage,
	error) {
	length, e := ReadVariableInt(r)
	if e != nil {
		return nil, fmt.Errorf("Couldn't read SysEx message length: %s", e)
	}
	data := make([]byte, length)
	_, e = r.Read(data)
	if e != nil {
		return nil, fmt.Errorf("Couldn't read SysEx message data: %s", e)
	}
	// Sanity check for the message format required by the spec.
	if (firstByte == 0xf0) && (data[len(data)-1] != 0xf7) {
		return nil, fmt.Errorf("SysEx message didn't end with 0xf7 byte")
	}
	// We won't include the trailing 0xf7 in here.
	return &SystemExclusiveMessage{
		DataBytes: data,
	}, nil
}

// Holds a meta-event type that we don't understand yet.
type GenericMetaEvent struct {
	EventType uint8
	Data      []byte
}

func (g *GenericMetaEvent) String() string {
	return fmt.Sprintf("Unknown meta-event. Type %d, size: %d bytes",
		g.EventType, len(g.Data))
}

// Takes a meta-event type and data and formats it into a slice of bytes that
// would be expected in an SMF file.
func formatMetaEventBytes(eventType uint8, data []byte) ([]byte, error) {
	var toReturn bytes.Buffer
	toReturn.WriteByte(0xff)
	toReturn.WriteByte(eventType)
	e := WriteVariableInt(&toReturn, uint32(len(data)))
	if e != nil {
		return nil, fmt.Errorf("Failed writing meta-event length: %s", e)
	}
	toReturn.Write(data)
	return toReturn.Bytes(), nil
}

func (g *GenericMetaEvent) SMFData(runningStatus *byte) ([]byte, error) {
	*runningStatus = 0
	return formatMetaEventBytes(g.EventType, g.Data)
}

// A meta-event holding a sequence number.
type SequenceNumberMetaEvent uint16

func (n SequenceNumberMetaEvent) String() string {
	return fmt.Sprintf("Sequence number: %d", uint16(n))
}

func (n SequenceNumberMetaEvent) SMFData(runningStatus *byte) ([]byte, error) {
	*runningStatus = 0
	return formatMetaEventBytes(0, []byte{uint8(n >> 8), uint8(n)})
}

// Parses a sequence number meta-event. Assumes the 0xff and 0x00 bytes have
// already been consumed.
func parseSequenceNumberMetaEvent(data []byte) (MIDIMessage, error) {
	if len(data) != 2 {
		return nil, fmt.Errorf("Bad sequence number event size: %d bytes",
			len(data))
	}
	// The sequence number is a 16-bit big endian integer.
	n := uint16(data[1])
	n |= uint16(data[0]) << 8
	return SequenceNumberMetaEvent(n), nil
}

type TextMetaEvent struct {
	// A byte between 0x01 and 0x0f that gives more information about the type
	// of text event this is.
	TextEventType uint8
	// The text data.
	Data []byte
}

func (t *TextMetaEvent) String() string {
	var eventType string
	switch t.TextEventType {
	case 0x1:
		eventType = "Generic text event"
	case 0x2:
		eventType = "Copyright notice"
	case 0x3:
		eventType = "Track/sequence name"
	case 0x4:
		eventType = "Instrument name"
	case 0x5:
		eventType = "Lyric"
	case 0x6:
		eventType = "Marker"
	case 0x7:
		eventType = "Cue point"
	default:
		eventType = fmt.Sprintf("Unknown text event type %d", t.TextEventType)
	}
	return fmt.Sprintf("%s: %s", eventType, t.Data)
}

func (t *TextMetaEvent) SMFData(runningStatus *byte) ([]byte, error) {
	*runningStatus = 0
	return formatMetaEventBytes(t.TextEventType, t.Data)
}

// Assumes r is at the start of a text meta event, and that the text event type
// has already been consumed. The text event type must be passed as the "b"
// argument.
func parseTextMetaEvent(eventType uint8, data []byte) (MIDIMessage, error) {
	return &TextMetaEvent{
		TextEventType: eventType,
		Data:          data,
	}, nil
}

// This represents a "MIDI Channel Prefix" meta-event, associating subsequent
// meta and sysex events with a channel number.
type ChannelPrefixMetaEvent uint8

func (c ChannelPrefixMetaEvent) String() string {
	return fmt.Sprintf("Channel prefix: %d", uint8(c))
}

func (c ChannelPrefixMetaEvent) SMFData(runningStatus *byte) ([]byte, error) {
	*runningStatus = 0
	return formatMetaEventBytes(0x20, []byte{byte(c)})
}

type EndOfTrackMetaEvent uint8

func (t EndOfTrackMetaEvent) String() string {
	return "End of track"
}

func (t EndOfTrackMetaEvent) SMFData(runningStatus *byte) ([]byte, error) {
	*runningStatus = 0
	return formatMetaEventBytes(0x2f, nil)
}

// Holds the 24-bit value for a "set tempo" meta-event. This contains the
// number of microseconds per quarter note.
type SetTempoMetaEvent uint32

func (t SetTempoMetaEvent) String() string {
	bpm := 60000000.0 / float32(t)
	return fmt.Sprintf("Set tempo to %d ms/quarter note (%f BPM)", uint32(t),
		bpm)
}

func (t SetTempoMetaEvent) SMFData(runningStatus *byte) ([]byte, error) {
	*runningStatus = 0
	if t >= 0xffffff {
		return nil, fmt.Errorf("Got set tempo value that's over 24 bits: 0x%x",
			uint32(t))
	}
	return formatMetaEventBytes(0x51, []byte{
		byte(t >> 16),
		byte(t >> 8),
		byte(t),
	})
}

func parseSetTempoMetaEvent(data []byte) (MIDIMessage, error) {
	if len(data) != 3 {
		return nil, fmt.Errorf("Expected 3 byte length for set tempo event, "+
			"got %d bytes", len(data))
	}
	toReturn := uint32(data[2])
	toReturn |= uint32(data[1]) << 8
	toReturn |= uint32(data[0]) << 16
	return SetTempoMetaEvent(toReturn), nil
}

// Holds an SMPTE offset meta-event's data. I may replace this with a format
// that's more human-readable in the future.
type SMPTEOffsetMetaEvent struct {
	Hours            uint8
	Minutes          uint8
	Seconds          uint8
	Frames           uint8
	FractionalFrames uint8
}

func (s *SMPTEOffsetMetaEvent) String() string {
	// The fractional frames specifies hundredths of a frame.
	frame := float32(s.Frames)
	frame += float32(s.FractionalFrames) / 100.0
	return fmt.Sprintf("SMPTE offset: %d:%d:%d, %f frames", s.Hours, s.Minutes,
		s.Seconds, frame)
}

func (s *SMPTEOffsetMetaEvent) SMFData(runningStatus *byte) ([]byte, error) {
	*runningStatus = 0
	return formatMetaEventBytes(0x54, []byte{s.Hours, s.Minutes, s.Seconds,
		s.Frames, s.FractionalFrames})
}

func parseSMPTEOffsetMetaEvent(data []byte) (MIDIMessage, error) {
	if len(data) != 5 {
		return nil, fmt.Errorf("Invalid SMPTE offset meta-event length: %d",
			len(data))
	}
	return &SMPTEOffsetMetaEvent{
		Hours:            data[0],
		Minutes:          data[1],
		Seconds:          data[2],
		Frames:           data[3],
		FractionalFrames: data[4],
	}, nil
}

type TimeSignatureMetaEvent struct {
	// The "denominator" is a negative power of 2; for example, if the
	// signature was 5/8 time then Numerator would be 5 and Denominator would
	// be 3.
	Numerator   uint8
	Denominator uint8
	// This is the number of MIDI clocks (24ths of a quarter note) per
	// metronome tick.  I don't really see why this is useful.
	ClocksPerMetronomeTick uint8
	// This is the number of notated 32nd notes per quarter note. I don't
	// really understand the purpose of this, either...
	Notated32ndNotesPerQuarterNote uint8
}

func (s *TimeSignatureMetaEvent) String() string {
	base := uint32(1) << uint32(s.Denominator)
	return fmt.Sprintf("Time signature: %d/%d time, %d clocks per metronome "+
		"tick, %d 32nd notes per notated quarter note", s.Numerator, base,
		s.ClocksPerMetronomeTick, s.Notated32ndNotesPerQuarterNote)
}

func (s *TimeSignatureMetaEvent) SMFData(runningStatus *byte) ([]byte, error) {
	*runningStatus = 0
	return formatMetaEventBytes(0x58, []byte{
		s.Numerator,
		s.Denominator,
		s.ClocksPerMetronomeTick,
		s.Notated32ndNotesPerQuarterNote,
	})
}

func parseTimeSignatureMetaEvent(data []byte) (MIDIMessage, error) {
	if len(data) != 4 {
		return nil, fmt.Errorf("Bad time signature meta-event size: %d",
			len(data))
	}
	return &TimeSignatureMetaEvent{
		Numerator:                      data[0],
		Denominator:                    data[1],
		ClocksPerMetronomeTick:         data[2],
		Notated32ndNotesPerQuarterNote: data[3],
	}, nil
}

type KeySignatureMetaEvent struct {
	// Valid range is from -7 to +7. Negative 7 indicates 7 flats, positive 7
	// indicates 7 sharps, and 0 indicates no sharps or flats.
	SharpOrFlatCount int8
	// This is true if the key signature is for a minor key.
	IsMinor bool
}

func (s *KeySignatureMetaEvent) String() string {
	sf := s.SharpOrFlatCount
	tmp := "sharps or flats"
	if sf < 0 {
		sf = -sf
		tmp = "flat"
	} else if sf > 0 {
		tmp = "sharp"
	}
	if sf > 1 {
		tmp += "s"
	}
	mm := "major"
	if s.IsMinor {
		mm = "minor"
	}
	return fmt.Sprintf("Key signature: %d %s, %s key", sf, tmp, mm)
}

func (s *KeySignatureMetaEvent) SMFData(runningStatus *byte) ([]byte, error) {
	*runningStatus = 0
	sf := s.SharpOrFlatCount
	if (sf < -7) || (sf > 7) {
		return nil, fmt.Errorf("Bad sharp or flat count in key signature: %d",
			sf)
	}
	mm := byte(0)
	if s.IsMinor {
		mm = byte(1)
	}
	return formatMetaEventBytes(0x59, []byte{byte(sf), mm})
}

func parseKeySignatureMetaEvent(data []byte) (MIDIMessage, error) {
	if len(data) != 2 {
		return nil, fmt.Errorf("Bad key signature meta-event size: %d",
			len(data))
	}
	sf := int8(data[0])
	if (sf < -7) || (sf > 7) {
		return nil, fmt.Errorf("Bad number of sharps or flats in key "+
			"signature: %d", sf)
	}
	if data[1] > 1 {
		return nil, fmt.Errorf("Invalid major/minor setting in key "+
			"signature: %d", data[1])
	}
	return &KeySignatureMetaEvent{
		SharpOrFlatCount: sf,
		IsMinor:          data[1] == 1,
	}, nil
}

// Parses a meta-event message in an SMF file. Returns an error if an unknown
// meta-event is encountered. Assumes the 0xff byte at the start of the message
// has already been consumed.
func parseMetaEvent(r io.Reader) (MIDIMessage, error) {
	eventType, e := readByte(r)
	if e != nil {
		return nil, fmt.Errorf("Failed reading meta-event type: %s", e)
	}
	eventLength, e := ReadVariableInt(r)
	if e != nil {
		return nil, fmt.Errorf("Failed reading meta-event length: %s", e)
	}
	var eventData []byte
	if eventLength != 0 {
		eventData = make([]byte, eventLength)
		_, e = r.Read(eventData)
		if e != nil {
			return nil, fmt.Errorf("Failed reading meta-event data: %s", e)
		}
	}
	if eventType == 0x00 {
		return parseSequenceNumberMetaEvent(eventData)
	}
	if (eventType >= 0x01) && (eventType <= 0x0f) {
		return parseTextMetaEvent(eventType, eventData)
	}
	if eventType == 0x20 {
		if eventLength != 1 {
			return nil, fmt.Errorf("Bad channel prefix meta-event length: %d",
				eventLength)
		}
		return ChannelPrefixMetaEvent(eventData[0]), nil
	}
	if eventType == 0x2f {
		if eventLength != 0 {
			return nil, fmt.Errorf("Bad end-of-track meta-event length: %d",
				eventLength)
		}
		return EndOfTrackMetaEvent(0), nil
	}
	if eventType == 0x51 {
		return parseSetTempoMetaEvent(eventData)
	}
	if eventType == 0x54 {
		return parseSMPTEOffsetMetaEvent(eventData)
	}
	if eventType == 0x58 {
		return parseTimeSignatureMetaEvent(eventData)
	}
	if eventType == 0x59 {
		return parseKeySignatureMetaEvent(eventData)
	}
	return &GenericMetaEvent{
		EventType: eventType,
		Data:      eventData,
	}, nil
}

// Holds a MIDI note value. The values corresponding to keys on a standard
// keyboard are 21 (A0) through 108 (C8).
type MIDINote uint8

func (n MIDINote) String() string {
	if (n < 21) || (n > 108) {
		return fmt.Sprintf("MIDI note %d", uint8(n))
	}
	notes := [...]string{"A", "A#", "B", "C", "C#", "D", "D#", "E", "F",
		"F#", "G", "G#"}
	index := (int(n) - 21) % 12
	octave := (int(n) - 12) / 12
	return fmt.Sprintf("%s%d", notes[index], octave)
}

type NoteOffEvent struct {
	Channel  uint8
	Note     MIDINote
	Velocity uint8
}

func (v *NoteOffEvent) String() string {
	return fmt.Sprintf("Channel %d: %s off, velocity = %d", v.Channel, v.Note,
		v.Velocity)
}

func (v *NoteOffEvent) SMFData(runningStatus *byte) ([]byte, error) {
	if v.Channel > 0xf {
		return nil, fmt.Errorf("Invalid note-off channel: %d", v.Channel)
	}
	if v.Note > 0x7f {
		return nil, fmt.Errorf("Invalid note-off note: %d", v.Note)
	}
	if v.Velocity > 0x7f {
		return nil, fmt.Errorf("Invalid note-off velocity: %d", v.Velocity)
	}
	status := uint8(0x80) | v.Channel
	// Omit the running status if it's the same, otherwise set the new running
	// status and include it in the output bytes.
	if status == *runningStatus {
		return []byte{byte(v.Note), v.Velocity}, nil
	}
	*runningStatus = status
	return []byte{status, byte(v.Note), v.Velocity}, nil
}

func parseNoteOffEvent(r io.Reader, firstByte, channel uint8) (MIDIMessage,
	error) {
	var n byte
	var e error
	// If the first byte was a status byte, then it has already been processed,
	// otherwise we were using running status and the first byte was the note.
	if firstByte <= 0x7f {
		n = firstByte
	} else {
		n, e = readByte(r)
	}
	if e != nil {
		return nil, fmt.Errorf("Failed reading note-off note: %s", e)
	}
	if n > 0x7f {
		return nil, fmt.Errorf("Invalid note-off note: %d", n)
	}
	v, e := readByte(r)
	if e != nil {
		return nil, fmt.Errorf("Failed reading note-off velocity: %s", e)
	}
	if v > 0x7f {
		return nil, fmt.Errorf("Invalid note-off velocity: %d", v)
	}
	return &NoteOffEvent{
		Channel:  channel,
		Note:     MIDINote(n),
		Velocity: v,
	}, nil
}

type NoteOnEvent struct {
	Channel  uint8
	Note     MIDINote
	Velocity uint8
}

func (v *NoteOnEvent) String() string {
	return fmt.Sprintf("Channel %d: %s on, velocity = %d", v.Channel, v.Note,
		v.Velocity)
}

func (v *NoteOnEvent) SMFData(runningStatus *byte) ([]byte, error) {
	if v.Channel > 0xf {
		return nil, fmt.Errorf("Invalid note-on channel: %d", v.Channel)
	}
	if v.Note > 0x7f {
		return nil, fmt.Errorf("Invalid note-on note: %d", v.Note)
	}
	if v.Velocity > 0x7f {
		return nil, fmt.Errorf("Invalid note-on velocity: %d", v.Velocity)
	}
	// This function is basically identical to its counterpart for NoteOffEvent
	// except for the status byte.
	status := uint8(0x90) | v.Channel
	if status == *runningStatus {
		return []byte{byte(v.Note), v.Velocity}, nil
	}
	*runningStatus = status
	return []byte{status, byte(v.Note), v.Velocity}, nil
}

func parseNoteOnEvent(r io.Reader, firstByte, channel uint8) (MIDIMessage,
	error) {
	// This, and basically every other channel-event parsing function works in
	// the same was as parseNoteOffEvent.
	var n uint8
	var e error
	if firstByte <= 0x7f {
		n = firstByte
	} else {
		n, e = readByte(r)
	}
	if e != nil {
		return nil, fmt.Errorf("Failed reading note-on note: %s", e)
	}
	if n > 0x7f {
		return nil, fmt.Errorf("Invalid note-on note: %d", n)
	}
	v, e := readByte(r)
	if e != nil {
		return nil, fmt.Errorf("Failed reading note-on velocity: %s", e)
	}
	if v > 0x7f {
		return nil, fmt.Errorf("Invalid note-on velocity: %d", v)
	}
	return &NoteOnEvent{
		Channel:  channel,
		Note:     MIDINote(n),
		Velocity: v,
	}, nil
}

// The aftertouch event is also known as a "polyphonic key pressure" event, but
// I'm using the word "aftertouch" because it's shorter in the source code.
type AftertouchEvent struct {
	Channel  uint8
	Note     MIDINote
	Pressure uint8
}

func (v *AftertouchEvent) String() string {
	return fmt.Sprintf("Channel %d: %s aftertouch pressure %d", v.Channel,
		v.Note, v.Pressure)
}

func (v *AftertouchEvent) SMFData(runningStatus *byte) ([]byte, error) {
	if v.Channel > 0xf {
		return nil, fmt.Errorf("Invalid aftertouch channel: %d", v.Channel)
	}
	if v.Note > 0x7f {
		return nil, fmt.Errorf("Invalid aftertouch note: %d", v.Note)
	}
	if v.Pressure > 0x7f {
		return nil, fmt.Errorf("Invalid aftertouch pressure: %d", v.Pressure)
	}
	status := uint8(0xa0) | v.Channel
	if status == *runningStatus {
		return []byte{byte(v.Note), v.Pressure}, nil
	}
	*runningStatus = status
	return []byte{status, byte(v.Note), v.Pressure}, nil
}

func parseAftertouchEvent(r io.Reader, firstByte, channel uint8) (MIDIMessage,
	error) {
	var n uint8
	var e error
	if firstByte <= 0x7f {
		n = firstByte
	} else {
		n, e = readByte(r)
	}
	if e != nil {
		return nil, fmt.Errorf("Failed reading aftertouch note: %s", e)
	}
	if n > 0x7f {
		return nil, fmt.Errorf("Invalid aftertouch note: %d", n)
	}
	p, e := readByte(r)
	if e != nil {
		return nil, fmt.Errorf("Failed reading aftertouch pressure: %s", e)
	}
	if p > 0x7f {
		return nil, fmt.Errorf("Invalid aftertouch pressure: %d", p)
	}
	return &AftertouchEvent{
		Channel:  channel,
		Note:     MIDINote(n),
		Pressure: p,
	}, nil
}

// This represents either a control-change message or a channel-mode message.
// It's a channel-mode message if 120 <= ControllerNumber <= 127.
type ControlChangeEvent struct {
	Channel          uint8
	ControllerNumber uint8
	Value            uint8
}

func (v *ControlChangeEvent) String() string {
	c := fmt.Sprintf("Channel %d: ", v.Channel)
	// First, we'll print the correct strings if this was a channel mode
	// message.
	switch v.ControllerNumber {
	case 120:
		return c + fmt.Sprintf("All sound off (v = %d)", v.Value)
	case 121:
		return c + fmt.Sprintf("Reset all controllers (v = %d)", v.Value)
	case 122:
		tmp := "off"
		if v.Value == 127 {
			tmp = "on"
		} else if v.Value != 0 {
			tmp = fmt.Sprintf("unknown setting %d", v.Value)
		}
		return c + fmt.Sprintf("Local control %s", tmp)
	case 123:
		return c + fmt.Sprintf("All notes off (v = %d)", v.Value)
	case 124:
		return c + fmt.Sprintf("Omni mode off (v = %d)", v.Value)
	case 125:
		return c + fmt.Sprintf("Omni mode on (v = %d)", v.Value)
	case 126:
		return c + fmt.Sprintf("Mono mode on (v = %d)", v.Value)
	case 127:
		return c + fmt.Sprintf("Poly mode on (v = %d)", v.Value)
	}
	return c + fmt.Sprintf("Control change, controller number %d, value %d",
		v.ControllerNumber, v.Value)
}

func (v *ControlChangeEvent) SMFData(runningStatus *byte) ([]byte, error) {
	if v.Channel > 0xf {
		return nil, fmt.Errorf("Invalid control-change channel: %d", v.Channel)
	}
	if v.ControllerNumber > 0x7f {
		return nil, fmt.Errorf("Invalid control-change controller: %d",
			v.ControllerNumber)
	}
	if v.Value > 0x7f {
		return nil, fmt.Errorf("Invalid control-change value: %d", v.Value)
	}
	status := byte(0xb0) | v.Channel
	if status == *runningStatus {
		return []byte{v.ControllerNumber, v.Value}, nil
	}
	*runningStatus = status
	return []byte{status, v.ControllerNumber, v.Value}, nil
}

func parseControlChangeEvent(r io.Reader, firstByte, channel uint8) (
	MIDIMessage, error) {
	var c uint8
	var e error
	if firstByte <= 0x7f {
		c = firstByte
	} else {
		c, e = readByte(r)
	}
	if e != nil {
		return nil, fmt.Errorf("Failed reading control-change controller "+
			"number: %s", e)
	}
	if c > 0x7f {
		return nil, fmt.Errorf("Invalid control-change controller number: %d",
			c)
	}
	v, e := readByte(r)
	if e != nil {
		return nil, fmt.Errorf("Failed reading control-change value: %s", e)
	}
	if v > 0x7f {
		return nil, fmt.Errorf("Invalid control-change value: %d", v)
	}
	return &ControlChangeEvent{
		Channel:          channel,
		ControllerNumber: c,
		Value:            v,
	}, nil
}

// This represents a program-change event, often used to set the "instrument"
// associated with a channel.
type ProgramChangeEvent struct {
	Channel uint8
	Value   uint8
}

func (v *ProgramChangeEvent) String() string {
	return fmt.Sprintf("Channel %d: program change to %d", v.Channel, v.Value)
}

func (v *ProgramChangeEvent) SMFData(runningStatus *byte) ([]byte, error) {
	if v.Channel > 0xf {
		return nil, fmt.Errorf("Invalid program-change channel: %d", v.Channel)
	}
	if v.Value > 0x7f {
		return nil, fmt.Errorf("Invalid program-change value: %d", v.Value)
	}
	status := byte(0xc0) | v.Channel
	if status == *runningStatus {
		return []byte{v.Value}, nil
	}
	*runningStatus = status
	return []byte{status, v.Value}, nil
}

func parseProgramChangeEvent(r io.Reader, firstByte, channel uint8) (
	MIDIMessage, error) {
	var v uint8
	var e error
	if firstByte <= 0x7f {
		v = firstByte
	} else {
		v, e = readByte(r)
	}
	if e != nil {
		return nil, fmt.Errorf("Failed reading program-change value: %s", e)
	}
	if v > 0x7f {
		return nil, fmt.Errorf("Invalid program-change value: %d", v)
	}
	return &ProgramChangeEvent{
		Channel: channel,
		Value:   v,
	}, nil
}

// This sets the maximum aftertouch pressure for the channel, or something like
// that. I don't really understand its purpose.
type ChannelPressureEvent struct {
	Channel uint8
	Value   uint8
}

func (v *ChannelPressureEvent) String() string {
	return fmt.Sprintf("Channel %d: Set channel pressure to %d", v.Channel,
		v.Value)
}

func (v *ChannelPressureEvent) SMFData(runningStatus *byte) ([]byte, error) {
	if v.Channel > 0xf {
		return nil, fmt.Errorf("Bad channel-pressure channel: %d", v.Channel)
	}
	if v.Value > 0x7f {
		return nil, fmt.Errorf("Bad channel-pressure value: %d", v.Value)
	}
	status := byte(0xd0) | v.Channel
	if status == *runningStatus {
		return []byte{v.Value}, nil
	}
	*runningStatus = status
	return []byte{status, v.Value}, nil
}

func parseChannelPressureEvent(r io.Reader, firstByte, channel uint8) (
	MIDIMessage, error) {
	var v uint8
	var e error
	if firstByte <= 0x7f {
		v = firstByte
	} else {
		v, e = readByte(r)
	}
	if e != nil {
		return nil, fmt.Errorf("Failed reading channel-pressure value: %s", e)
	}
	if v > 0x7f {
		return nil, fmt.Errorf("Invalid channel-pressure value: %d", v)
	}
	return &ChannelPressureEvent{
		Channel: channel,
		Value:   v,
	}, nil
}

// Holds a pitch-bend event. The "center" value is 0x2000. The value can be
// at most 14 bits.
type PitchBendEvent struct {
	Channel uint8
	Value   uint16
}

func (v *PitchBendEvent) String() string {
	return fmt.Sprintf("Channel %d: Pitch bend value %d", v.Channel, v.Value)
}

func (v *PitchBendEvent) SMFData(runningStatus *byte) ([]byte, error) {
	if v.Channel > 0xf {
		return nil, fmt.Errorf("Invalid pitch-bend channel: %d", v.Channel)
	}
	if v.Value > 0x3fff {
		return nil, fmt.Errorf("Invalid pitch-bend value: %d", v.Value)
	}
	lowBits := uint8(v.Value & 0x7f)
	highBits := uint8(v.Value >> 7)
	status := byte(0xe0) | v.Channel
	if status == *runningStatus {
		return []byte{lowBits, highBits}, nil
	}
	*runningStatus = status
	return []byte{status, lowBits, highBits}, nil
}

func parsePitchBendEvent(r io.Reader, firstByte, channel uint8) (MIDIMessage,
	error) {
	var lowBits uint8
	var e error
	if firstByte <= 0x7f {
		lowBits = firstByte
	} else {
		lowBits, e = readByte(r)
	}
	if e != nil {
		return nil, fmt.Errorf("Couldn't read pitch-bend low bits: %s", e)
	}
	if lowBits > 0x7f {
		return nil, fmt.Errorf("Invalid pitch-bend low bits: %d", lowBits)
	}
	highBits, e := readByte(r)
	if e != nil {
		return nil, fmt.Errorf("Couldn't read pitch-bend high bits: %s", e)
	}
	if highBits > 0x7f {
		return nil, fmt.Errorf("Invalid pitch-bend high bits: %d", highBits)
	}
	value := uint16(highBits) << 7
	value |= uint16(lowBits)
	return &PitchBendEvent{
		Channel: channel,
		Value:   value,
	}, nil
}

func parseChannelMessage(r io.Reader, firstByte byte, runningStatus *byte) (
	MIDIMessage, error) {
	status := firstByte
	// Use the running status if the first byte is not a status byte.
	if (status & 0x80) == 0 {
		status = *runningStatus
	} else {
		// We got a new status byte, so update the running status.
		*runningStatus = status
	}
	// If "status" is not a status byte, then neither the first byte nor the
	// running status indicated a valid status.
	if (status & 0x80) == 0 {
		return nil, fmt.Errorf("Can't parse a channel message without a " +
			"valid status or running status")
	}
	channel := status & 0xf
	switch status & 0xf0 {
	case 0x80:
		return parseNoteOffEvent(r, firstByte, channel)
	case 0x90:
		return parseNoteOnEvent(r, firstByte, channel)
	case 0xa0:
		return parseAftertouchEvent(r, firstByte, channel)
	case 0xb0:
		return parseControlChangeEvent(r, firstByte, channel)
	case 0xc0:
		return parseProgramChangeEvent(r, firstByte, channel)
	case 0xd0:
		return parseChannelPressureEvent(r, firstByte, channel)
	case 0xe0:
		return parsePitchBendEvent(r, firstByte, channel)
	}
	return nil, fmt.Errorf("Parsing MIDI channel message not yet implemented.")
}

// Parses and returns the MIDI message at the start of r. Requires a running
// status byte that may be modified by calling this function. If a running
// status is not set, then runningStatus must be zero.
func ReadSMFMessage(r io.Reader, runningStatus *byte) (MIDIMessage, error) {
	firstByte, e := readByte(r)
	if e != nil {
		return nil, fmt.Errorf("Failed reading start of MIDI message: %s", e)
	}
	if (firstByte == 0xf0) || (firstByte == 0xf7) {
		// Sysex messages reset running status.
		*runningStatus = 0
		return parseSystemExclusiveMessage(r, firstByte)
	}
	if firstByte == 0xff {
		// Meta-events also reset running status.
		*runningStatus = 0
		return parseMetaEvent(r)
	}
	if (firstByte & 0xf0) == 0xf0 {
		// TODO: Eventually support the remaining messages here, e.g. more
		// system common messages or real-time messages.
		return nil, fmt.Errorf("Status byte 0x%02x not yet supported",
			firstByte)
	}
	return parseChannelMessage(r, firstByte, runningStatus)
}
