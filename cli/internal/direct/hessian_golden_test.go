package direct

import (
	"encoding/hex"
	"testing"
)

func TestHessianJavaGoldenDecode(t *testing.T) {
	cases := []struct {
		name string
		hex  string
		want func(t *testing.T, got interface{})
	}{
		{
			name: "string-emoji",
			hex:  "0461eda0bdedb98262",
			want: func(t *testing.T, got interface{}) {
				if got != "a🙂b" {
					t.Fatalf("got %#v", got)
				}
			},
		},
		{
			name: "long",
			hex:  "4c06058ae04ec22000",
			want: func(t *testing.T, got interface{}) {
				if got != int64(433905635109773312) {
					t.Fatalf("got %#v", got)
				}
			},
		},
		{
			name: "integer",
			hex:  "95",
			want: func(t *testing.T, got interface{}) {
				if got != int64(5) {
					t.Fatalf("got %#v", got)
				}
			},
		},
		{
			name: "double-whole",
			hex:  "6902",
			want: func(t *testing.T, got interface{}) {
				if got != float64(2) {
					t.Fatalf("got %#v", got)
				}
			},
		},
		{
			name: "big-decimal",
			hex:  "4fa46a6176612e6d6174682e426967446563696d616c910576616c75656f9007313030302e3530",
			want: func(t *testing.T, got interface{}) {
				fields := goldenObjectFields(t, got, "java.math.BigDecimal")
				if fields["value"] != "1000.50" {
					t.Fatalf("fields = %#v", fields)
				}
			},
		},
		{
			name: "list-with-null",
			hex:  "566e034ee10374776f7a",
			want: func(t *testing.T, got interface{}) {
				items := goldenListItems(t, got)
				if len(items) != 3 || items[0] != nil || items[1] != int64(1) || items[2] != "two" {
					t.Fatalf("items = %#v", items)
				}
			},
		},
		{
			name: "map-long-key",
			hex:  "4d7400176a6176612e7574696c2e4c696e6b6564486173684d6170e705736576656e046e616d6505616c6963657a",
			want: func(t *testing.T, got interface{}) {
				entries := goldenMapEntries(t, got)
				if entries["7"] != "seven" || entries["name"] != "alice" {
					t.Fatalf("entries = %#v", entries)
				}
			},
		},
		{
			name: "bytes",
			hex:  "230102ff",
			want: func(t *testing.T, got interface{}) {
				b, ok := got.([]byte)
				if !ok || hex.EncodeToString(b) != "0102ff" {
					t.Fatalf("got %#v", got)
				}
			},
		},
		{
			name: "query-response",
			hex:  "4fb34865737369616e436f6e747261637448656c706572245175657279526573706f6e736593077375636365737306616d6f756e7404746167736f90544fa46a6176612e6d6174682e426967446563696d616c910576616c75656f910b3131333739352e323438355674001a6a6176612e7574696c2e4172726179732441727261794c6973746e02014101427a",
			want: func(t *testing.T, got interface{}) {
				fields := goldenObjectFields(t, got, "HessianContractHelper$QueryResponse")
				if fields["success"] != true {
					t.Fatalf("fields = %#v", fields)
				}
				amount := goldenObjectFields(t, fields["amount"], "java.math.BigDecimal")
				if amount["value"] != "113795.2485" {
					t.Fatalf("amount = %#v", amount)
				}
				tags := goldenListItems(t, fields["tags"])
				if len(tags) != 2 || tags[0] != "A" || tags[1] != "B" {
					t.Fatalf("tags = %#v", tags)
				}
			},
		},
		{
			name: "date",
			hex:  "640000000000000000",
			want: func(t *testing.T, got interface{}) {
				if got != int64(0) {
					t.Fatalf("date = %#v, want epoch millis", got)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := readGoldenHessian(t, tc.hex)
			tc.want(t, got)
		})
	}
}

func TestHessianJavaGoldenDocumentsKnownBigIntegerPresentationGap(t *testing.T) {
	got := readGoldenHessian(t, "4fa46a6176612e6d6174682e426967496e746567657296067369676e756d08626974436f756e74096269744c656e6774680c6c6f776573745365744269741266697273744e6f6e7a65726f496e744e756d036d61676f909190909090567400045b696e746e02497fffffff8f7a")
	fields := goldenObjectFields(t, got, "java.math.BigInteger")
	if _, ok := fields["value"]; ok {
		t.Fatalf("BigInteger unexpectedly rendered as value; remove this known-gap test")
	}
	if fields["signum"] != int64(1) || fields["mag"] == nil {
		t.Fatalf("fields = %#v", fields)
	}
}

func readGoldenHessian(t *testing.T, text string) interface{} {
	t.Helper()
	data, err := hex.DecodeString(text)
	if err != nil {
		t.Fatalf("decode golden hex: %v", err)
	}
	got, err := (&reader{data: data}).readValue()
	if err != nil {
		t.Fatalf("readValue: %v; data=%s", err, text)
	}
	return got
}

func goldenListItems(t *testing.T, got interface{}) []interface{} {
	t.Helper()
	if items, ok := got.([]interface{}); ok {
		return items
	}
	obj, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("got %T, want list", got)
	}
	items, ok := obj["items"].([]interface{})
	if !ok {
		t.Fatalf("got %#v, want list items", got)
	}
	return items
}

func goldenMapEntries(t *testing.T, got interface{}) map[string]interface{} {
	t.Helper()
	entries, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("got %T, want map", got)
	}
	if nested, ok := entries["entries"].(map[string]interface{}); ok {
		return nested
	}
	return entries
}

func goldenObjectFields(t *testing.T, got interface{}, wantType string) map[string]interface{} {
	t.Helper()
	obj, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("got %T, want object %s", got, wantType)
	}
	if obj["type"] != wantType {
		t.Fatalf("type = %#v, want %s; object=%#v", obj["type"], wantType, obj)
	}
	fields, ok := obj["fields"].(map[string]interface{})
	if !ok {
		t.Fatalf("fields missing in %#v", obj)
	}
	return fields
}
