package schema

import (
	"path/filepath"
	"testing"
)

func TestParserGoldenSalesFacade(t *testing.T) {
	root := filepath.Join("testdata", "golden", "sales")
	idx, err := BuildIndex(Project{
		Name:            "sales",
		WorkspaceRoot:   root,
		ServicePrefixes: []string{"com.acme.sales.facade."},
	})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	desc, err := Describe(idx, "com.acme.sales.facade.PortfolioFacade", "queryPortfolioLatestAsset")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if len(desc.Methods) != 1 {
		t.Fatalf("methods = %#v", desc.Methods)
	}
	method := desc.Methods[0]
	if method.ReturnType != "Result<List<AssetDTO>>" {
		t.Fatalf("return type = %q", method.ReturnType)
	}
	if method.Summary != "查询最新资产" {
		t.Fatalf("summary = %q", method.Summary)
	}
	if len(method.Parameters) != 2 {
		t.Fatalf("parameters = %#v", method.Parameters)
	}
	if method.Parameters[0] != (Parameter{Name: "request", Type: "AssetQuery"}) {
		t.Fatalf("first parameter = %#v", method.Parameters[0])
	}
	if method.Parameters[1] != (Parameter{Name: "filters", Type: "Map<String, List<Long>>"}) {
		t.Fatalf("second parameter = %#v", method.Parameters[1])
	}
	if got := method.Imports["AssetQuery"]; got != "com.acme.sales.dto.AssetQuery" {
		t.Fatalf("AssetQuery import = %q", got)
	}
	query := desc.Types["com.acme.sales.dto.AssetQuery"]
	if query.Type == "" {
		t.Fatalf("missing AssetQuery schema: %#v", desc.Types)
	}
	wantFields := map[string]string{
		"mpCode":  "Long",
		"tags":    "List<String>",
		"filters": "Map<String, List<Long>>",
		"payload": "byte[]",
	}
	for _, field := range query.Fields {
		if want, ok := wantFields[field.Name]; ok && field.Type == want {
			delete(wantFields, field.Name)
		}
	}
	if len(wantFields) != 0 {
		t.Fatalf("missing fields = %#v; got %#v", wantFields, query.Fields)
	}
	results := Search(idx, "最新资产", 5, false)
	if len(results) != 1 || results[0].Method != "queryPortfolioLatestAsset" {
		t.Fatalf("search results = %#v", results)
	}
}

func TestParserGoldenModernJavaFacade(t *testing.T) {
	root := filepath.Join("testdata", "golden", "modern")
	idx, err := BuildIndex(Project{
		Name:            "modern",
		WorkspaceRoot:   root,
		ServicePrefixes: []string{"com.acme.modern.facade."},
	})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	desc, err := Describe(idx, "com.acme.modern.facade.PositionFacade", "queryPositions")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if len(desc.Methods) != 1 {
		t.Fatalf("methods = %#v", desc.Methods)
	}
	method := desc.Methods[0]
	if method.ReturnType != "Result<Page<PositionRecord>>" {
		t.Fatalf("return type = %q", method.ReturnType)
	}
	if method.Summary != "查询持仓快照" {
		t.Fatalf("summary = %q", method.Summary)
	}
	if len(method.Parameters) != 2 {
		t.Fatalf("parameters = %#v", method.Parameters)
	}
	if method.Parameters[0] != (Parameter{Name: "query", Type: "PositionQuery"}) {
		t.Fatalf("first parameter = %#v", method.Parameters[0])
	}
	if method.Parameters[1] != (Parameter{Name: "accountIds", Type: "List<Long>"}) {
		t.Fatalf("second parameter = %#v", method.Parameters[1])
	}

	assertFields(t, desc.Types["com.acme.modern.dto.PositionQuery"], map[string]string{
		"mpCode":        "Long",
		"states":        "List<String>",
		"status":        "PositionStatus",
		"amountFilters": "Map<String, List<BigDecimal>>",
	})
	status := desc.Types["com.acme.modern.dto.PositionStatus"]
	if status.Kind != "enum" {
		t.Fatalf("PositionStatus kind = %q; schema=%#v", status.Kind, status)
	}
	if len(status.EnumValues) != 2 || status.EnumValues[0] != "ACTIVE" || status.EnumValues[1] != "INACTIVE" {
		t.Fatalf("PositionStatus enum values = %#v", status.EnumValues)
	}
	assertFields(t, desc.Types["com.acme.modern.dto.PositionRecord"], map[string]string{
		"id":     "Long",
		"amount": "BigDecimal",
		"tags":   "List<String>",
	})
	assertFields(t, desc.Types["com.acme.modern.dto.Page"], map[string]string{
		"records": "List<T>",
		"total":   "int",
	})
	assertFields(t, desc.Types["com.acme.modern.dto.Result"], map[string]string{
		"success": "boolean",
		"data":    "T",
	})
}

func TestParserGoldenOverloadedFacade(t *testing.T) {
	root := filepath.Join("testdata", "golden", "modern")
	idx, err := BuildIndex(Project{
		Name:            "modern",
		WorkspaceRoot:   root,
		ServicePrefixes: []string{"com.acme.modern.facade."},
	})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	desc, err := Describe(idx, "com.acme.modern.facade.OrderFacade", "queryOrder")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if len(desc.Methods) != 2 {
		t.Fatalf("methods = %#v", desc.Methods)
	}
	signatures := map[string]bool{}
	for _, method := range desc.Methods {
		if len(method.Parameters) != 1 {
			t.Fatalf("method parameters = %#v", method)
		}
		signatures[method.Parameters[0].Type] = true
	}
	if !signatures["String"] || !signatures["OrderQuery"] {
		t.Fatalf("signatures = %#v; methods = %#v", signatures, desc.Methods)
	}
	assertFields(t, desc.Types["com.acme.modern.dto.OrderDTO"], map[string]string{
		"orderId": "String",
		"userId":  "Long",
	})
	assertFields(t, desc.Types["com.acme.modern.dto.OrderQuery"], map[string]string{
		"orderId": "String",
		"userId":  "Long",
	})
}

func TestParserGoldenWildcardImport(t *testing.T) {
	root := filepath.Join("testdata", "golden", "wildcard")
	idx, err := BuildIndex(Project{
		Name:            "wildcard",
		WorkspaceRoot:   root,
		ServicePrefixes: []string{"com.acme.wildcard.facade."},
	})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	desc, err := Describe(idx, "com.acme.wildcard.facade.WildcardFacade", "query")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if len(desc.Methods) != 1 {
		t.Fatalf("methods = %#v", desc.Methods)
	}
	method := desc.Methods[0]
	if method.ReturnType != "WildResp" {
		t.Errorf("ReturnType = %q, want WildResp", method.ReturnType)
	}
	if method.Parameters[0].Type != "WildReq" {
		t.Errorf("param[0].Type = %q, want WildReq", method.Parameters[0].Type)
	}
	if method.Imports["WildReq"] != "com.acme.wildcard.dto.WildReq" {
		t.Errorf("imports[WildReq] = %q, want com.acme.wildcard.dto.WildReq (wildcard expanded?)", method.Imports["WildReq"])
	}
	if method.Imports["WildResp"] != "com.acme.wildcard.dto.WildResp" {
		t.Errorf("imports[WildResp] = %q, want com.acme.wildcard.dto.WildResp", method.Imports["WildResp"])
	}
	req := desc.Types["com.acme.wildcard.dto.WildReq"]
	if req.Type == "" {
		t.Fatalf("WildReq schema missing in desc.Types = %v", desc.Types)
	}
	assertFields(t, req, map[string]string{"key": "String", "mpCode": "Long"})

	resp := desc.Types["com.acme.wildcard.dto.WildResp"]
	if resp.Type == "" {
		t.Fatalf("WildResp schema missing in desc.Types = %v", desc.Types)
	}
	assertFields(t, resp, map[string]string{"success": "boolean", "messages": "List<String>"})
}

func TestParserGoldenInnerClass(t *testing.T) {
	root := filepath.Join("testdata", "golden", "inner")
	idx, err := BuildIndex(Project{
		Name:            "inner",
		WorkspaceRoot:   root,
		ServicePrefixes: []string{"com.acme.inner.facade."},
	})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	desc, err := Describe(idx, "com.acme.inner.facade.OuterFacade", "listPages")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	method := desc.Methods[0]
	if method.ReturnType != "List<PageResult>" {
		t.Errorf("ReturnType = %q, want List<PageResult>", method.ReturnType)
	}
	if method.Parameters[0].Type != "PageQuery" {
		t.Errorf("param.Type = %q, want PageQuery", method.Parameters[0].Type)
	}
	query := desc.Types["com.acme.inner.facade.PageQuery"]
	if query.Type == "" {
		t.Fatalf("PageQuery schema missing: %v", desc.Types)
	}
	assertFields(t, query, map[string]string{"mpCode": "Long", "offset": "int"})

	result := desc.Types["com.acme.inner.facade.PageResult"]
	if result.Type == "" {
		t.Fatalf("PageResult schema missing: %v", desc.Types)
	}
	assertFields(t, result, map[string]string{"name": "String", "tags": "List<String>"})
}

func TestParserGoldenWildcardExpansionDoesNotPolluteUnrelatedPackages(t *testing.T) {
	root := filepath.Join("testdata", "golden", "wildcard")
	idx, err := BuildIndex(Project{
		Name:            "wildcard",
		WorkspaceRoot:   root,
		ServicePrefixes: []string{"com.acme.wildcard.facade."},
	})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	desc, _ := Describe(idx, "com.acme.wildcard.facade.WildcardFacade", "query")
	method := desc.Methods[0]
	if len(method.Imports) != 3 {
		t.Fatalf("imports = %v, want exactly 3 (top-level of wildcard package only)", method.Imports)
	}
	want := []string{"WildReq", "WildResp", "WildContainer"}
	for _, w := range want {
		if _, ok := method.Imports[w]; !ok {
			t.Errorf("expected import %q missing: %v", w, method.Imports)
		}
	}
	if _, ok := method.Imports["WildInner"]; ok {
		t.Errorf("WildInner (nested) should NOT be in wildcard imports: %v", method.Imports)
	}
	if _, ok := method.Imports["Unrelated"]; ok {
		t.Errorf("Unrelated (different package) should NOT be in wildcard imports: %v", method.Imports)
	}
}

func assertFields(t *testing.T, schema TypeSchema, wantFields map[string]string) {
	t.Helper()
	if schema.Type == "" {
		t.Fatalf("missing schema; want fields %#v", wantFields)
	}
	remaining := map[string]string{}
	for name, typ := range wantFields {
		remaining[name] = typ
	}
	for _, field := range schema.Fields {
		if want, ok := remaining[field.Name]; ok && field.Type == want {
			delete(remaining, field.Name)
		}
	}
	if len(remaining) != 0 {
		t.Fatalf("missing fields = %#v; got %#v", remaining, schema.Fields)
	}
}
