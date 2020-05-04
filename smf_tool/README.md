SMF Tool
========

This is a tool to manipulate or view data in standard MIDI files.

Usage
-----

To simply parse a file and print a list of its contents to `stdout`:
```
./smf_tool -input_file <my_file.mid> -dump_events
```

The tool also supports inserting events into existing tracks:

```
./smf_tool -input_file <my_file.mid> -output_file <new_file.mid> \
    -track 2 -position 2 \
    -new_event "00 C0 0F"
```

The above command uses the following flags:
 - `-input_file`: This must be provided, and specifies the original SMF file to
   be modified.
 - `-output_file`: This indicates the output file to be created. If this isn't
   provided, no output file will be written. (To overwrite the input file,
   simply pass the same file name to `-input_file` and `-output_file`. Passing
   `-dump_events` rather than an `-output_file` is a good way to "preview" any
   changes without writing them to a file, should you want to do so.
 - `-track 2`: Selects track 2. This tool numbers tracks starting from 1, but
   the underlying `github.com/yalue/midi` library numbers then starting from 0.
 - `-position 2`: Insert the new event after position 2 in the track. (So,
   after insertion, the new event will be at index 2.)
 - `-new_event "00 C0 0e"`: The `-new_event` flag takes a string of hex data,
   which will be parsed as a MIDI event. The data can not use running status
   and must include a delta-time.  In this case, the new event has a time
   delta of 0 (the first `00`), specifies a "program change event" to channel 0
   (the `C0` byte), and sets the new "program" to the "Tubular bells"
   instrument (byte `0e` = #14 starting from 0, so instrument 15 in general
   MIDI).

Run the tool with `-help` for a full list of options.

