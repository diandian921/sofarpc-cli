package app

import (
	"testing"

	"github.com/diandian921/sofarpc-mcp/internal/presentation"
)

// TestBuildInvokeDataAssertionsPass: all assertions satisfied -> failCount 0 and the
// outcomes are emitted alongside the result.
func TestBuildInvokeDataAssertionsPass(t *testing.T) {
	flattened := map[string]interface{}{"status": "ACTIVE", "name": "x"}
	exists := true
	data, fail := buildInvokeData(flattened, flattened, false, 5, nil,
		[]presentation.Assertion{
			{Path: "$.status", Equals: "ACTIVE"},
			{Path: "$.name", Exists: &exists},
		}, "")
	if fail != 0 {
		t.Fatalf("expected 0 failures, got %d", fail)
	}
	outcomes, ok := data["assertions"].([]presentation.AssertionOutcome)
	if !ok || len(outcomes) != 2 {
		t.Fatalf("expected 2 assertion outcomes, got %v", data["assertions"])
	}
	for _, o := range outcomes {
		if !o.Passed {
			t.Errorf("assertion %s should pass", o.Path)
		}
	}
	if data["result"] == nil {
		t.Error("data.result must be present on pass")
	}
}

// TestBuildInvokeDataAssertionFailFlipsCount: a failing assertion bumps failCount but
// keeps data.result and data.assertions so the caller can emit them on isError.
func TestBuildInvokeDataAssertionFailFlipsCount(t *testing.T) {
	flattened := map[string]interface{}{"status": "CLOSED"}
	data, fail := buildInvokeData(flattened, flattened, false, 1, nil,
		[]presentation.Assertion{{Path: "$.status", Equals: "ACTIVE"}}, "")
	if fail != 1 {
		t.Fatalf("expected 1 failure, got %d", fail)
	}
	if data["result"] == nil || data["assertions"] == nil {
		t.Error("failed assertion must still carry data.result and data.assertions")
	}
}

// TestBuildInvokeDataResultPathHitAndMiss: a matching resultPath narrows data.result
// to the subtree; a miss nulls it. resultPathMatched reflects which happened.
func TestBuildInvokeDataResultPathHitAndMiss(t *testing.T) {
	flattened := map[string]interface{}{"user": map[string]interface{}{"id": "u1"}}

	data, _ := buildInvokeData(flattened, flattened, false, 0, nil, nil, "$.user.id")
	if data["result"] != "u1" {
		t.Errorf("resultPath hit should narrow result to \"u1\", got %v", data["result"])
	}
	if matched, _ := data["resultPathMatched"].(bool); !matched {
		t.Error("resultPathMatched should be true on hit")
	}

	data, _ = buildInvokeData(flattened, flattened, false, 0, nil, nil, "$.user.missing")
	if data["result"] != nil {
		t.Errorf("resultPath miss should null the result, got %v", data["result"])
	}
	if matched, ok := data["resultPathMatched"].(bool); !ok || matched {
		t.Error("resultPathMatched should be false on miss")
	}
}

// TestBuildInvokeDataAssertionsSeeFullBeforeResultPathNarrows pins the ordering:
// assertions evaluate on the full result, resultPath only narrows what is returned.
func TestBuildInvokeDataAssertionsSeeFullBeforeResultPathNarrows(t *testing.T) {
	flattened := map[string]interface{}{
		"status": "ACTIVE",
		"user":   map[string]interface{}{"id": "u1"},
	}
	data, fail := buildInvokeData(flattened, flattened, false, 0, nil,
		[]presentation.Assertion{{Path: "$.status", Equals: "ACTIVE"}}, "$.user")
	if fail != 0 {
		t.Fatalf("assertion on $.status should pass even though resultPath narrows to $.user, got %d failures", fail)
	}
	sub, ok := data["result"].(map[string]interface{})
	if !ok || sub["id"] != "u1" {
		t.Errorf("result should be narrowed to the $.user subtree, got %v", data["result"])
	}
}

// TestBuildInvokeDataOmitsOptionalKeys: with no assertions and no resultPath, those
// keys are absent; rawResult is included only when requested.
func TestBuildInvokeDataOmitsOptionalKeys(t *testing.T) {
	flattened := map[string]interface{}{"a": 1}
	raw := map[string]interface{}{"a": int64(1), "_extra": "tree"}

	data, _ := buildInvokeData(flattened, raw, true, 0, nil, nil, "")
	if data["rawResult"] == nil {
		t.Error("rawResult=true must include data.rawResult")
	}
	if _, ok := data["assertions"]; ok {
		t.Error("no assertions should omit data.assertions")
	}
	if _, ok := data["resultPathMatched"]; ok {
		t.Error("no resultPath should omit data.resultPathMatched")
	}

	data, _ = buildInvokeData(flattened, raw, false, 0, nil, nil, "")
	if _, ok := data["rawResult"]; ok {
		t.Error("rawResult=false should omit data.rawResult")
	}
}
