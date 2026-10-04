package app

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
)

// probeMP4 extracts width, height, and duration (seconds) from MP4 data.
// If any value cannot be determined, the default values are returned.
func probeMP4(data []byte, defaultWidth, defaultHeight, defaultDuration int) (int, int, int) {
	width, height, duration := defaultWidth, defaultHeight, defaultDuration

	r := bytes.NewReader(data)
	for {
		var boxHeader [8]byte
		if _, err := io.ReadFull(r, boxHeader[:]); err != nil {
			break
		}
		boxSize := int64(binary.BigEndian.Uint32(boxHeader[0:4]))
		boxType := string(boxHeader[4:8])

		headerSize := int64(8)
		if boxSize == 1 {
			var extSize [8]byte
			if _, err := io.ReadFull(r, extSize[:]); err != nil {
				break
			}
			boxSize = int64(binary.BigEndian.Uint64(extSize[:]))
			headerSize = 16
		} else if boxSize == 0 {
			boxSize = int64(len(data)) - (r.Size() - int64(r.Len()) - headerSize)
		}

		payloadSize := boxSize - headerSize
		if payloadSize < 0 || payloadSize > int64(r.Len()) {
			break
		}

		if boxType == "moov" {
			moovData := make([]byte, payloadSize)
			if _, err := io.ReadFull(r, moovData); err != nil {
				break
			}
			w, h, d := parseMoov(moovData)
			if w > 0 {
				width = w
			}
			if h > 0 {
				height = h
			}
			if d > 0 {
				duration = d
			}
			break
		}

		if _, err := r.Seek(payloadSize, io.SeekCurrent); err != nil {
			break
		}
	}

	return width, height, duration
}

func parseMoov(data []byte) (width, height, duration int) {
	r := bytes.NewReader(data)
	for {
		var boxHeader [8]byte
		if _, err := io.ReadFull(r, boxHeader[:]); err != nil {
			break
		}
		boxSize := int64(binary.BigEndian.Uint32(boxHeader[0:4]))
		boxType := string(boxHeader[4:8])

		headerSize := int64(8)
		if boxSize == 1 {
			var extSize [8]byte
			if _, err := io.ReadFull(r, extSize[:]); err != nil {
				break
			}
			boxSize = int64(binary.BigEndian.Uint64(extSize[:]))
			headerSize = 16
		}

		payloadSize := boxSize - headerSize
		if payloadSize < 0 || payloadSize > int64(r.Len()) {
			break
		}

		payload := make([]byte, payloadSize)
		if _, err := io.ReadFull(r, payload); err != nil {
			break
		}

		switch boxType {
		case "mvhd":
			d := parseMvhd(payload)
			if d > 0 {
				duration = d
			}
		case "trak":
			w, h := parseTrak(payload)
			if w > 0 && h > 0 {
				width = w
				height = h
			}
		}
	}
	return width, height, duration
}

func parseMvhd(data []byte) int {
	if len(data) < 24 {
		return 0
	}
	version := data[0]
	if version == 0 {
		timescale := binary.BigEndian.Uint32(data[12:16])
		dur := binary.BigEndian.Uint32(data[16:20])
		if timescale > 0 {
			return int(math.Round(float64(dur) / float64(timescale)))
		}
	} else if version == 1 && len(data) >= 32 {
		timescale := binary.BigEndian.Uint32(data[20:24])
		dur := binary.BigEndian.Uint64(data[24:32])
		if timescale > 0 {
			return int(math.Round(float64(dur) / float64(timescale)))
		}
	}
	return 0
}

func parseTrak(data []byte) (int, int) {
	r := bytes.NewReader(data)
	for {
		var boxHeader [8]byte
		if _, err := io.ReadFull(r, boxHeader[:]); err != nil {
			break
		}
		boxSize := int64(binary.BigEndian.Uint32(boxHeader[0:4]))
		boxType := string(boxHeader[4:8])

		headerSize := int64(8)
		if boxSize == 1 {
			var extSize [8]byte
			if _, err := io.ReadFull(r, extSize[:]); err != nil {
				break
			}
			boxSize = int64(binary.BigEndian.Uint64(extSize[:]))
			headerSize = 16
		}

		payloadSize := boxSize - headerSize
		if payloadSize < 0 || payloadSize > int64(r.Len()) {
			break
		}

		payload := make([]byte, payloadSize)
		if _, err := io.ReadFull(r, payload); err != nil {
			break
		}

		if boxType == "tkhd" && len(payload) >= 8 {
			w := int(binary.BigEndian.Uint32(payload[len(payload)-8:len(payload)-4]) >> 16)
			h := int(binary.BigEndian.Uint32(payload[len(payload)-4:]) >> 16)
			if w > 0 && h > 0 {
				return w, h
			}
		}
	}
	return 0, 0
}
