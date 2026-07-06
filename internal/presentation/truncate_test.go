package presentation

import (
	"reflect"
	"testing"
)

func TestTruncateArraysCutsLongArraysWithMarker(t *testing.T) {
	items := make([]interface{}, 250)
	for i := range items {
		items[i] = i
	}
	out, cut := TruncateArrays(items, 200)
	if !cut {
		t.Fatal("expected cut=true for a 250-item array")
	}
	arr, ok := out.([]interface{})
	if !ok || len(arr) != 201 {
		t.Fatalf("expected 200 kept items + 1 marker, got %d", len(arr))
	}
	if arr[0] != 0 || arr[199] != 199 {
		t.Fatalf("kept items must preserve order and index paths: %v %v", arr[0], arr[199])
	}
	marker, ok := arr[200].(map[string]interface{})
	if !ok {
		t.Fatalf("last element must be the $truncated marker, got %T", arr[200])
	}
	info, ok := marker["$truncated"].(map[string]interface{})
	if !ok || info["omittedItems"] != 50 || info["totalItems"] != 250 {
		t.Fatalf("marker payload wrong: %v", marker)
	}
}

func TestTruncateArraysRecursesIntoMapsAndNestedArrays(t *testing.T) {
	long := make([]interface{}, 5)
	for i := range long {
		long[i] = i
	}
	in := map[string]interface{}{
		"list": []interface{}{
			map[string]interface{}{"inner": long},
		},
	}
	out, cut := TruncateArrays(in, 3)
	if !cut {
		t.Fatal("expected nested array to be cut")
	}
	inner := out.(map[string]interface{})["list"].([]interface{})[0].(map[string]interface{})["inner"].([]interface{})
	if len(inner) != 4 {
		t.Fatalf("expected 3 kept + marker, got %d", len(inner))
	}
}

func TestTruncateArraysLeavesSmallTreesUntouched(t *testing.T) {
	in := map[string]interface{}{
		"list":  []interface{}{1, 2, 3},
		"value": "x",
	}
	out, cut := TruncateArrays(in, 200)
	if cut {
		t.Fatal("expected cut=false for a small tree")
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("small tree must be structurally unchanged:\n got  %v\n want %v", out, in)
	}
}
