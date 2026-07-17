package direct

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestReaderRejectsUntrustedContainerLengths(t *testing.T) {
	large := make([]byte, 4)
	binary.BigEndian.PutUint32(large, uint32(maxHessianContainerItems+1))
	negative := []byte{0xff, 0xff, 0xff, 0xff}

	tests := []struct {
		name string
		data []byte
	}{
		{name: "variable list too large", data: append([]byte{'V', 'l'}, large...)},
		{name: "class definition negative", data: append([]byte{'O', 0x90, 'I'}, negative...)},
		// fixed-list type ref 0 is valid after the leading typed empty list records it.
		{name: "fixed list negative", data: []byte{'V', 't', 0, 1, 'x', 0x6e, 0, 'z', 'v', 0x90, 'I', 0xff, 0xff, 0xff, 0xff}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &reader{data: tc.data}
			_, err := r.readValue()
			if err == nil {
				// The fixed-list case stores a type in the first value, so read the
				// malicious second value after the valid prefix.
				_, err = r.readValue()
			}
			if err == nil {
				t.Fatal("expected malformed container length to be rejected")
			}
			if !strings.Contains(err.Error(), "length") && !strings.Contains(err.Error(), "budget") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestReaderRejectsLengthBeyondRemainingInput(t *testing.T) {
	_, err := (&reader{data: []byte{'V', 0x6e, 3, 'N'}}).readValue()
	if err == nil || !strings.Contains(err.Error(), "remaining input") {
		t.Fatalf("err = %v, want remaining-input error", err)
	}
}

func FuzzReaderMalformedContainers(f *testing.F) {
	f.Add([]byte{'V', 'l', 0x7f, 0xff, 0xff, 0xff})
	f.Add([]byte{'O', 0x90, 'I', 0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{'V', 0x6e, 3, 'N'})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			t.Skip()
		}
		_, _ = (&reader{data: data}).readValue()
	})
}
