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
