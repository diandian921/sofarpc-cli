package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sofarpc/cli/internal/appconfig"
	"github.com/sofarpc/cli/internal/javavalue"
	"github.com/sofarpc/cli/internal/schema"
)

type fakeStore struct {
	cfg appconfig.Config
}

func (s fakeStore) Load() (appconfig.Config, error) {
	return s.cfg, nil
}

type fakeSource struct {
	desc schema.Description
}

func (s fakeSource) Describe(ctx context.Context, projectName string, project appconfig.Project, service, method string) (schema.Description, error) {
	return s.desc, nil
}

func TestRPCParamTypeForMethodExpandsImportedDTO(t *testing.T) {
	method := schema.Method{
		Package: "com.example.facade",
		Imports: map[string]string{
			"UserRequest": "com.example.model.UserRequest",
		},
		Parameters: []schema.Parameter{{Name: "request", Type: "UserRequest"}},
	}

	if got := rpcParamTypeForMethod("UserRequest", method); got != "com.example.model.UserRequest" {
		t.Fatalf("rpcParamTypeForMethod imported DTO = %q", got)
	}
	if got := rpcParamTypeForMethod("SamePackageRequest", method); got != "com.example.facade.SamePackageRequest" {
		t.Fatalf("rpcParamTypeForMethod same package DTO = %q", got)
	}
	if got := rpcParamTypeForMethod("Long", method); got != "java.lang.Long" {
		t.Fatalf("rpcParamTypeForMethod Long = %q", got)
	}
	if !sameParamTypes(method, []string{"com.example.model.UserRequest"}) {
		t.Fatalf("sameParamTypes should match FQN parameter")
	}
}

func TestMethodSignaturesIncludesOverloadCandidates(t *testing.T) {
	methods := []schema.Method{
		{
			Package:    "com.example",
			Method:     "query",
			Parameters: []schema.Parameter{{Name: "id", Type: "String"}},
		},
		{
			Package:    "com.example",
			Method:     "query",
			Parameters: []schema.Parameter{{Name: "request", Type: "QueryRequest"}},
		},
	}
	got := methodSignatures(methods)
	if !strings.Contains(got, "query(java.lang.String id)") || !strings.Contains(got, "query(com.example.QueryRequest request)") {
		t.Fatalf("signatures = %q", got)
	}
}

func TestTypedValueForParamAddsDTOFieldTypesWithoutMagicKeys(t *testing.T) {
	method := schema.Method{
		Package: "com.example.api",
		Imports: map[string]string{
			"UserRequest": "com.example.model.UserRequest",
		},
		Parameters: []schema.Parameter{{Name: "request", Type: "UserRequest"}},
	}
	desc := schema.Description{Types: map[string]schema.TypeSchema{
		"com.example.model.UserRequest": {
			Type: "com.example.model.UserRequest",
			Kind: "class",
			Fields: []schema.Field{
				{Name: "id", Type: "Long"},
				{Name: "ratio", Type: "Double"},
			},
		},
	}}
	typed := typedValueForParam(map[string]interface{}{"id": json.Number("5"), "ratio": json.Number("2.0")}, "UserRequest", method, desc)
	if typed.Kind != javavalue.KindObject || typed.JavaType != "com.example.model.UserRequest" {
		t.Fatalf("typed value = %#v", typed)
	}
	if typed.Fields["id"].JavaType != "java.lang.Long" || typed.Fields["ratio"].JavaType != "java.lang.Double" {
		t.Fatalf("fields = %#v", typed.Fields)
	}
	if _, exists := typed.Fields["__fieldTypes"]; exists {
		t.Fatalf("__fieldTypes should not be represented as a user field: %#v", typed.Fields)
	}
}

func TestTypedValueForJavaTypeKeepsByteArrayScalar(t *testing.T) {
	typed := typedValueForJavaType([]interface{}{json.Number("1"), json.Number("2"), json.Number("255")}, "byte[]", nil, 0)
	if typed.Kind != javavalue.KindScalar || typed.JavaType != "byte[]" {
		t.Fatalf("typed value = %#v", typed)
	}
}

func TestTypedValueForJavaTypeEncodesEnumAsJavaObject(t *testing.T) {
	types := map[string]schema.TypeSchema{
		"com.example.Status": {
			Type:       "com.example.Status",
			Kind:       "enum",
			EnumValues: []string{"ACTIVE", "INACTIVE"},
		},
	}
	typed := typedValueForJavaType("ACTIVE", "com.example.Status", types, 0)
	if typed.Kind != javavalue.KindObject || typed.JavaType != "com.example.Status" {
		t.Fatalf("typed enum = %#v", typed)
	}
	name := typed.Fields["name"]
	if name.Kind != javavalue.KindScalar || name.JavaType != "java.lang.String" || name.Scalar != "ACTIVE" {
		t.Fatalf("typed enum name = %#v", name)
	}

	wrapped := typedValueForJavaType(map[string]interface{}{"name": "INACTIVE"}, "com.example.Status", types, 0)
	if wrapped.Fields["name"].Scalar != "INACTIVE" {
		t.Fatalf("wrapped enum name = %#v", wrapped.Fields["name"])
	}

	nullEnum := typedValueForJavaType(nil, "com.example.Status", types, 0)
	if nullEnum.Kind != javavalue.KindScalar || nullEnum.JavaType != "com.example.Status" || nullEnum.Scalar != nil {
		t.Fatalf("null enum = %#v", nullEnum)
	}
}

func TestPlanNamedArgumentsUsesSourceIndexPort(t *testing.T) {
	cfg := appconfig.Config{
		Projects: map[string]appconfig.Project{
			"user": {WorkspaceRoot: "/unused"},
		},
		Servers: map[string]appconfig.Server{
			"user-test": {
				Address:     "127.0.0.1:12200",
				Project:     "user",
				Protocol:    "bolt",
				TimeoutMS:   5000,
				AppName:     "test",
				Attachments: map[string]string{"tenant": "blue"},
			},
		},
	}
	desc := schema.Description{
		Methods: []schema.Method{{
			Package:    "com.example.api",
			Imports:    map[string]string{"UserRequest": "com.example.model.UserRequest"},
			Method:     "query",
			Parameters: []schema.Parameter{{Name: "request", Type: "UserRequest"}},
		}},
		Types: map[string]schema.TypeSchema{
			"com.example.model.UserRequest": {
				Type:   "com.example.model.UserRequest",
				Kind:   "class",
				Fields: []schema.Field{{Name: "id", Type: "Long"}},
			},
		},
	}
	service := New(fakeStore{cfg: cfg})
	service.Source = fakeSource{desc: desc}
	plan, err := service.PlanInvocation(context.Background(), InvocationInput{
		Server:         "user-test",
		Service:        "com.example.api.UserFacade",
		Method:         "query",
		NamedArguments: map[string]interface{}{"id": json.Number("5")},
	})
	if err != nil {
		t.Fatalf("PlanInvocation: %v", err)
	}
	if got := plan.Method.ParamTypes; len(got) != 1 || got[0] != "com.example.model.UserRequest" {
		t.Fatalf("paramTypes = %#v", got)
	}
	if plan.Arguments[0].Kind != javavalue.KindObject || plan.Arguments[0].Fields["id"].JavaType != "java.lang.Long" {
		t.Fatalf("arguments = %#v", plan.Arguments)
	}
	req := directRequestFromPlan(plan)
	if req.Attachments["tenant"] != "blue" {
		t.Fatalf("direct request attachments = %#v", req.Attachments)
	}
	plan.Endpoint.Attachments["tenant"] = "mutated"
	if req.Attachments["tenant"] != "blue" {
		t.Fatalf("direct request attachments should be copied, got %#v", req.Attachments)
	}
}

func TestPlanExplicitPrimitiveArgumentsSkipSchema(t *testing.T) {
	cfg := appconfig.Config{
		Projects: map[string]appconfig.Project{
			"user": {WorkspaceRoot: "/path/that/does/not/exist"},
		},
		Servers: map[string]appconfig.Server{
			"user-test": {
				Address:   "127.0.0.1:12200",
				Project:   "user",
				Protocol:  "bolt",
				TimeoutMS: 5000,
				AppName:   "test",
			},
		},
	}
	service := New(fakeStore{cfg: cfg})
	plan, err := service.PlanInvocation(context.Background(), InvocationInput{
		Server:              "user-test",
		Service:             "com.example.UserService",
		Method:              "getUser",
		ParamTypes:          []string{"java.lang.String"},
		OrderedArguments:    []interface{}{"u001"},
		HasOrderedArguments: true,
	})
	if err != nil {
		t.Fatalf("PlanInvocation: %v", err)
	}
	if len(plan.Arguments) != 1 || plan.Arguments[0].JavaType != "java.lang.String" {
		t.Fatalf("arguments = %#v", plan.Arguments)
	}
	if len(plan.Warnings) != 0 {
		t.Fatalf("warnings = %#v", plan.Warnings)
	}
}

func TestTypedValueForJavaTypeHasDepthGuard(t *testing.T) {
	types := map[string]schema.TypeSchema{
		"com.example.Node": {
			Type:   "com.example.Node",
			Kind:   "class",
			Fields: []schema.Field{{Name: "next", Type: "Node"}},
		},
	}
	root := map[string]interface{}{}
	current := root
	for i := 0; i < maxTypePlanDepth+16; i++ {
		next := map[string]interface{}{}
		current["next"] = next
		current = next
	}
	got := typedValueForJavaType(root, "com.example.Node", types, 0)
	if got.Kind != javavalue.KindObject {
		t.Fatalf("typed kind = %q", got.Kind)
	}
}
