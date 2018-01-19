package netframe

import (
	"encoding/binary"
	"bytes"
)

var g_hh = []byte{0x03, 0x02}
var g_tt = []byte{0x05, 0x04}

const (
	g_ver uint8 = 1
	g_hhl uint8 = 6
)

type NetFrame struct {
	data []byte
}

func (frame *NetFrame) Pack(data []byte) ([]byte, int) {
	buf := new(bytes.Buffer)
	var buflen uint16 = uint16(len(data))
	binary.Write(buf, binary.LittleEndian, g_hh)
	binary.Write(buf, binary.LittleEndian, g_ver)
	binary.Write(buf, binary.LittleEndian, g_hhl)
	binary.Write(buf, binary.LittleEndian, buflen)
	binary.Write(buf, binary.LittleEndian, data)
	var ck uint16 = uint16(10 + buflen)
	binary.Write(buf, binary.LittleEndian, ck)
	binary.Write(buf, binary.LittleEndian, g_tt)
	return buf.Bytes(), buf.Len()
}

func (frame *NetFrame) AppendData(data []byte) {
	frame.data = append(frame.data, data...)
}

func (frame *NetFrame) Unpack(data []byte) (bool, int) {
	if len(frame.data) < 10 {
		return false, 0
	}

	buf := bytes.NewBuffer(frame.data)
	frame.data = frame.data[:0]
	result := false
	datasize := 0
	for {
		hh := make([]byte, 2)
		binary.Read(buf, binary.LittleEndian, &hh)
		if !bytes.Equal(g_hh, hh)  {
			continue
		}
		var ver uint8
		binary.Read(buf, binary.LittleEndian, &ver)
		if ver != g_ver {
			continue
		}
		var hhlen uint8
		binary.Read(buf, binary.LittleEndian, &hhlen)
		if hhlen != g_hhl {
			continue
		}
		var packlen uint16
		binary.Read(buf, binary.LittleEndian, &packlen)
		packbuf := make([]byte, packlen)
		binary.Read(buf, binary.LittleEndian, &packbuf)
		var cck uint16
		binary.Read(buf, binary.LittleEndian, &cck)
		tt := make([]byte, 2)
		binary.Read(buf, binary.LittleEndian, &tt)
		if !bytes.Equal(tt, g_tt) {
			continue
		}

		copy(data, packbuf)
		result = true
		datasize = len(packbuf)
		frame.data = append(frame.data, buf.Bytes()...)
		break
	}

	return result, datasize
}
