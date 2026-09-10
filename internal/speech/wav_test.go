package speech

import (
	"encoding/binary"
	"testing"
)

func TestWavFromPCM16(t *testing.T) {
	t.Parallel()

	pcm := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	wav := wavFromPCM16(pcm, pcmSampleRate, pcmChannels)

	if len(wav) != 44+len(pcm) {
		t.Fatalf("len = %d, want %d", len(wav), 44+len(pcm))
	}

	if string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" || string(wav[36:40]) != "data" {
		t.Fatalf("bad header: %q", wav[:44])
	}

	if rate := binary.LittleEndian.Uint32(wav[24:28]); rate != pcmSampleRate {
		t.Errorf("rate = %d, want %d", rate, pcmSampleRate)
	}

	if size := binary.LittleEndian.Uint32(wav[40:44]); int(size) != len(pcm) {
		t.Errorf("data size = %d, want %d", size, len(pcm))
	}
}
