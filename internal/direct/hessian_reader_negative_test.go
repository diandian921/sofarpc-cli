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

func TestReaderAllowsBatchBeyondLegacyItemBudget(t *testing.T) {
	const legacyItemBudget = 1 << 20
	n := legacyItemBudget + 1
	data := make([]byte, 0, 6+n)
	data = append(data, 'V', 'l')
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(n))
	data = append(data, length...)
	data = append(data, make([]byte, n)...)
	for i := 6; i < len(data); i++ {
		data[i] = 'N'
	}

	value, err := (&reader{data: data}).readValue()
	if err != nil {
		t.Fatalf("decode %d-item list above legacy budget: %v", n, err)
	}
	items, ok := value.([]interface{})
	if !ok || len(items) != n {
		t.Fatalf("decoded list = %T len %d, want []interface{} len %d", value, len(items), n)
	}
}

func TestReaderEnforcesNewAggregateItemBudget(t *testing.T) {
	r := &reader{data: make([]byte, maxHessianTotalItems+1)}
	if err := r.reserveContainerItems("list", (1<<20)+1, 1); err != nil {
		t.Fatalf("aggregate count above legacy budget should now pass: %v", err)
	}
	r.items = maxHessianTotalItems - 1
	if err := r.reserveContainerItems("list", 1, 1); err != nil {
		t.Fatalf("exact aggregate budget should pass: %v", err)
	}
	if err := r.reserveContainerItems("list", 1, 1); err == nil || !strings.Contains(err.Error(), "budget exceeded") {
		t.Fatalf("new aggregate budget must remain enforced, got %v", err)
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
