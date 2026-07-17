package javavalue

import "testing"

func TestBaseJavaType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"String", "String"},
		{" final java.util.List<java.lang.String>[] ", "java.util.List"},
		{"byte[][]", "byte"},
		{"java.lang.Byte [] []", "java.lang.Byte"},
		{"Map<String, List<Long>>", "Map"},
		{"? extends Item", "? extends Item"},
		{"finalized.Type", "finalized.Type"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := BaseJavaType(tt.input); got != tt.want {
			t.Errorf("BaseJavaType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsByteArrayType(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"byte[]", true},
		{"final byte []", true},
		{"java.lang.Byte[]", true},
		{" final\tjava.lang.Byte [ ] ", true},
		{"byte[][]", false},
		{"java.lang.Byte", false},
		{"Byte[]", false},
		{"finalized.byte[]", false},
	}
	for _, tt := range tests {
		if got := IsByteArrayType(tt.input); got != tt.want {
			t.Errorf("IsByteArrayType(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
