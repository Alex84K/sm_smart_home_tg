package app

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestProbeMP4Fallback(t *testing.T) {
	// Empty data should return default fallback
	w, h, d := probeMP4([]byte("not-an-mp4"), 1920, 1080, 30)
	if w != 1920 || h != 1080 || d != 30 {
		t.Fatalf("expected fallback (1920, 1080, 30), got (%d, %d, %d)", w, h, d)
	}
}

func TestProbeMP4Valid(t *testing.T) {
	var buf bytes.Buffer

	// Write ftyp box
	ftyp := []byte("isom\x00\x00\x02\x00isomiso2mp41")
	_ = binary.Write(&buf, binary.BigEndian, uint32(8+len(ftyp)))
	buf.WriteString("ftyp")
	buf.Write(ftyp)

	// Write mvhd payload: version 0 (1 byte) + flags (3 bytes) + creation (4) + mod (4) + timescale 1000 (4) + duration 45000 (4)
	var mvhdPayload bytes.Buffer
	mvhdPayload.Write([]byte{0, 0, 0, 0})                           // v0
	mvhdPayload.Write(make([]byte, 8))                              // creation, mod
	_ = binary.Write(&mvhdPayload, binary.BigEndian, uint32(1000))  // timescale
	_ = binary.Write(&mvhdPayload, binary.BigEndian, uint32(45000)) // duration (45s)
	mvhdPayload.Write(make([]byte, 80))                             // rest of mvhd

	var mvhdBox bytes.Buffer
	_ = binary.Write(&mvhdBox, binary.BigEndian, uint32(8+mvhdPayload.Len()))
	mvhdBox.WriteString("mvhd")
	mvhdBox.Write(mvhdPayload.Bytes())

	// Write tkhd payload: ending with width 1280 (16.16) and height 720 (16.16)
	var tkhdPayload bytes.Buffer
	tkhdPayload.Write(make([]byte, 76)) // header + matrix etc.
	_ = binary.Write(&tkhdPayload, binary.BigEndian, uint32(1280<<16))
	_ = binary.Write(&tkhdPayload, binary.BigEndian, uint32(720<<16))

	var tkhdBox bytes.Buffer
	_ = binary.Write(&tkhdBox, binary.BigEndian, uint32(8+tkhdPayload.Len()))
	tkhdBox.WriteString("tkhd")
	tkhdBox.Write(tkhdPayload.Bytes())

	var trakBox bytes.Buffer
	_ = binary.Write(&trakBox, binary.BigEndian, uint32(8+tkhdBox.Len()))
	trakBox.WriteString("trak")
	trakBox.Write(tkhdBox.Bytes())

	// moov box containing mvhd and trak
	var moovBox bytes.Buffer
	moovPayloadLen := mvhdBox.Len() + trakBox.Len()
	_ = binary.Write(&moovBox, binary.BigEndian, uint32(8+moovPayloadLen))
	moovBox.WriteString("moov")
	moovBox.Write(mvhdBox.Bytes())
	moovBox.Write(trakBox.Bytes())

	buf.Write(moovBox.Bytes())

	w, h, d := probeMP4(buf.Bytes(), 1920, 1080, 30)
	if w != 1280 {
		t.Errorf("expected width 1280, got %d", w)
	}
	if h != 720 {
		t.Errorf("expected height 720, got %d", h)
	}
	if d != 45 {
		t.Errorf("expected duration 45, got %d", d)
	}
}
