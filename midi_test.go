package midi

import (
	"bytes"
	"io"
	"testing"
)

func TestVariableIntRead(t *testing.T) {
	expected := []uint32{
		0x00000000,
		0x00000040,
		0x0000007F,
		0x00000080,
		0x00002000,
		0x00003FFF,
		0x00004000,
		0x00100000,
		0x001FFFFF,
		0x00200000,
		0x08000000,
		0x0FFFFFFF,
	}
	// This should contain the variable-length integers equivalent to those in
	// the "expected" slice, followed by an invalid integer that's too long,
	// and an invalid integer that hits EOF too soon.
	data := []byte{
		0x00,
		0x40,
		0x7F,
		0x81, 0x00,
		0xC0, 0x00,
		0xFF, 0x7F,
		0x81, 0x80, 0x00,
		0xC0, 0x80, 0x00,
		0xFF, 0xFF, 0x7F,
		0x81, 0x80, 0x80, 0x00,
		0xC0, 0x80, 0x80, 0x00,
		0xFF, 0xFF, 0xFF, 0x7F,
		0xff, 0xff, 0xff, 0x80, 0xff,
	}
	r := bytes.NewReader(data)
	for _, v := range expected {
		valueRead, e := ReadVariableInt(r)
		if e != nil {
			t.Logf("Failed reading variable-length int 0x%08x: %s\n", v, e)
			t.FailNow()
		}
		if valueRead != v {
			t.Logf("Read wrong value for variable-length int. Expected "+
				"0x%08x, got 0x%08x.\n", v, valueRead)
			t.FailNow()
		}
	}
	_, e := ReadVariableInt(r)
	if e == nil {
		t.Logf("Didn't get expected error for reading an invalid int.\n")
		t.FailNow()
	}
	t.Logf("Got expected error for invalid variable-length int: %s\n", e)
	_, e = ReadVariableInt(r)
	if e == nil {
		t.Logf("Didn't get expected error for reading an incomplete int.\n")
		t.FailNow()
	}
	// Remember, we don't want to get an io.EOF error on an integer that's
	// incomplete--this would make it harder to tell the difference between a
	// track that ends normally and one that ends in the middle of a time
	// delta.
	if e == io.EOF {
		t.Logf("Got io.EOF from reading an incomplete int.\n")
		t.FailNow()
	}
	t.Logf("Got expected error for incomplete int: %s\n", e)
	_, e = ReadVariableInt(r)
	if e != io.EOF {
		if e != nil {
			t.Logf("Didn't get io.EOF when trying to read an int at EOF. "+
				"Instead got this error: %s\n", e)
		} else {
			t.Logf("Didn't get an error when trying to read an int at EOF.\n")
		}
		t.FailNow()
	}
	t.Logf("Got expected EOF error when reading an int at EOF: %s\n", e)
}

func TestVariableIntWrite(t *testing.T) {
	// This will basically be the TestVariableIntRead test, except in reverse.
	data := []uint32{
		0x00000000,
		0x00000040,
		0x0000007F,
		0x00000080,
		0x00002000,
		0x00003FFF,
		0x00004000,
		0x00100000,
		0x001FFFFF,
		0x00200000,
		0x08000000,
		0x0FFFFFFF,
	}
	expected := []byte{
		0x00,
		0x40,
		0x7F,
		0x81, 0x00,
		0xC0, 0x00,
		0xFF, 0x7F,
		0x81, 0x80, 0x00,
		0xC0, 0x80, 0x00,
		0xFF, 0xFF, 0x7F,
		0x81, 0x80, 0x80, 0x00,
		0xC0, 0x80, 0x80, 0x00,
		0xFF, 0xFF, 0xFF, 0x7F,
	}
	var output bytes.Buffer
	for _, v := range data {
		e := WriteVariableInt(&output, v)
		if e != nil {
			t.Logf("Failed writing variable int 0x%08x: %s\n", v, e)
			t.FailNow()
		}
	}
	for i, b := range output.Bytes() {
		if b != expected[i] {
			t.Logf("Got different output byte at offset %d: wanted 0x%02x, "+
				"got 0x%02x\n", i, expected[i], b)
			t.FailNow()
		}
	}
	e := WriteVariableInt(&output, 0x10000000)
	if e == nil {
		t.Logf("Didn't get expected error for writing int that's too big.\n")
		t.FailNow()
	}
	t.Logf("Got expected error when writing int that's too big: %s\n", e)
}
