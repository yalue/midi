package midi

import (
	"bytes"
	"testing"
)

func TestParseSMFFile(t *testing.T) {
	// This SMF file is defined in the midi specification, in the section on
	// SMF files.
	smfData := []byte{
		// MThd
		0x4d, 0x54, 0x68, 0x64,
		// Chunk length
		0, 0, 0, 6,
		// Format 1
		0, 1,
		// Four tracks,
		0, 4,
		// 96 ticks per quarter note
		0, 0x60,
		// Track chunk for the time signature/tempo track, starting with the
		// MTrk:
		0x4d, 0x54, 0x72, 0x6b,
		// Chunk length:
		0, 0, 0, 0x14,
		// Time signature, with delta-time
		0, 0xff, 0x58, 4, 4, 2, 0x18, 8,
		// Tempo
		0, 0xff, 0x51, 3, 7, 0xa1, 0x20,
		// End of track
		0x83, 0, 0xff, 0x2f, 0,
		// The first music track, starting with MTrk
		0x4d, 0x54, 0x72, 0x6b,
		// The chunk length
		0, 0, 0, 0x10,
		// Change program for channel 0 to 5.
		0, 0xc0, 5,
		// Note 0x4c on, at time delta, setting running status.
		0x81, 0x40, 0x90, 0x4c, 0x20,
		// Note off, using running status for note on, but velocity=0
		0x81, 0x40, 0x4c, 0,
		// End of track.
		0, 0xff, 0x2f, 0,
		// Track chunk for second music track, starting with MTrk:
		0x4d, 0x54, 0x72, 0x6b,
		// Chunk length
		0, 0, 0, 0xf,
		// Program change for channel 1, to 0x2e
		0, 0xc1, 0x2e,
		// Note 0x43 on
		0x60, 0x91, 0x43, 0x40,
		// Note 0x43 off, using running status.
		0x82, 0x20, 0x43, 0,
		// End of track
		0, 0xff, 0x2f, 0,
		// The third track, starting with MTrk:
		0x4d, 0x54, 0x72, 0x6b,
		// Chunk length
		0, 0, 0, 0x15,
		// Program change for channel 2 to 0x46.
		0, 0xc2, 0x46,
		// Note 0x30 on
		0, 0x92, 0x30, 0x60,
		// Note 0x3c on, using running status
		0, 0x3c, 0x60,
		// Note 0x30 off, using running status
		0x83, 0, 0x30, 0,
		// Note 0x3c off, using running status
		0, 0x3c, 0,
		// End of track
		0, 0xff, 0x2f, 0,
	}
	r := bytes.NewReader(smfData)
	smfFile, e := ParseSMFFile(r)
	if e != nil {
		t.Logf("Failed parsing SMF file: %s\n", e)
		t.FailNow()
	}
	if len(smfFile.Tracks) != 4 {
		t.Logf("Expected 4 SMF file tracks, got %d\n", e)
		t.FailNow()
	}
	for trackNumber, track := range smfFile.Tracks {
		t.Logf("Track %d, %d messages:\n", trackNumber, len(track.Messages))
		for i := range track.Messages {
			t.Logf("  %d. Time-delta %d: %s\n", i+1, track.TimeDeltas[i],
				track.Messages[i].String())
		}
	}
	// This simple file should match exactly when we re-write it, since it uses
	// running status and doesn't do anything odd.
	var outputFile bytes.Buffer
	e = smfFile.WriteToFile(&outputFile)
	if e != nil {
		t.Logf("Failed writing SMF file: %s\n", e)
		t.FailNow()
	}
	outputData := outputFile.Bytes()
	if len(outputData) != len(smfData) {
		t.Logf("Got incorrect output file length: expected %d, got %d\n",
			len(smfData), len(outputData))
		t.FailNow()
	}
	for i := range outputData {
		if outputData[i] != smfData[i] {
			t.Logf("Written data doesn't match original file at byte %d: "+
				"got 0x%02x, expected 0x%02x\n", i, outputData[i], smfData[i])
			t.Fail()
			break
		}
	}
	t.Logf("The written output file matches the input SMF data!\n")
}
