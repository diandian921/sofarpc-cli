package app

import (
	"context"
	"fmt"
	"time"

	"github.com/diandian921/sofarpc-cli/cli/internal/appconfig"
	"github.com/diandian921/sofarpc-cli/cli/internal/direct"
	"github.com/diandian921/sofarpc-cli/cli/internal/javavalue"
	"github.com/diandian921/sofarpc-cli/cli/internal/presentation"
	"github.com/diandian921/sofarpc-cli/cli/internal/schema"
)

func (s *Service) PlanInvocation(ctx context.Context, input InvocationInput) (InvocationPlan, error) {
	if err := ctx.Err(); err != nil {
		return InvocationPlan{}, err
	}
	start := time.Now()
	if input.Service == "" {
		return InvocationPlan{}, &DomainError{Kind: ErrServiceNotFound, Message: "service is required"}
	}
	if input.Method == "" {
		return InvocationPlan{}, &DomainError{Kind: ErrMethodNotFound, Message: "method is required"}
	}
	if input.Address != "" {
		return s.planExplicitAddressInvocation(input, start)
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return InvocationPlan{}, err
	}
	serverName, server, hasServer, err := resolveServer(cfg, input.Project, input.Server, true)
	if err != nil {
		return InvocationPlan{}, err
	}
	if !hasServer {
		return InvocationPlan{}, fmt.Errorf("server is required")
	}
	projectName, project, err := resolveProject(cfg, input.Project, serverName)
	if err != nil {
		return InvocationPlan{}, err
	}
	args, paramTypes, warnings, err := s.planArguments(ctx, projectName, project, input.Service, input.Method, input)
	if err != nil {
		return InvocationPlan{}, err
	}
	timeoutMS := input.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = server.TimeoutMS
	}
	endpoint := endpointFromServer(serverName, server, timeoutMS)
	return InvocationPlan{
		Project:   ProjectRef{Name: projectName, Info: project},
		Server:    serverName,
		Endpoint:  endpoint,
		Service:   input.Service,
		Method:    MethodSignature{Name: input.Method, ParamTypes: paramTypes},
		Arguments: args,
		Timeout:   time.Duration(timeoutMS) * time.Millisecond,
		TimeoutMS: timeoutMS,
		RawResult: input.RawResult,
		Warnings:  warnings,
		Diagnostics: Diagnostics{
			Timing: map[string]int64{
				"planMs": time.Since(start).Milliseconds(),
			},
			Resolution: map[string]interface{}{
				"project":        projectName,
				"server":         serverName,
				"service":        input.Service,
				"method":         input.Method,
				"endpointSource": "configured-server",
			},
			Warnings: warnings,
		},
	}, nil
}

func (s *Service) planExplicitAddressInvocation(input InvocationInput, start time.Time) (InvocationPlan, error) {
	if !input.HasOrderedArguments {
		return InvocationPlan{}, &DomainError{Kind: ErrArgumentTypeMismatch, Message: "orderedArguments are required when address is explicit"}
	}
	if len(input.ParamTypes) == 0 {
		return InvocationPlan{}, &DomainError{Kind: ErrArgumentTypeMismatch, Message: "paramTypes are required when address is explicit"}
	}
	if len(input.ParamTypes) != len(input.OrderedArguments) {
		return InvocationPlan{}, &DomainError{Kind: ErrArgumentTypeMismatch, Message: fmt.Sprintf("paramTypes length (%d) does not match orderedArguments length (%d)", len(input.ParamTypes), len(input.OrderedArguments)), Details: map[string]interface{}{"paramTypeCount": len(input.ParamTypes), "argumentCount": len(input.OrderedArguments)}}
	}
	timeoutMS := input.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = appconfig.DefaultServerTimeoutMS
	}
	protocol := input.Protocol
	if protocol == "" {
		protocol = appconfig.DefaultServerProtocol
	}
	appName := input.AppName
	if appName == "" {
		appName = appconfig.DefaultServerAppName
	}
	endpoint := Endpoint{
		Address:     input.Address,
		Protocol:    protocol,
		TimeoutMS:   timeoutMS,
		AppName:     appName,
		Attachments: map[string]string{},
	}
	warnings := []PlanWarning(nil)
	if needsSchemaAnnotation(input.ParamTypes) {
		warnings = append(warnings, PlanWarning{
			Code:    "SCHEMA_ANNOTATION_SKIPPED",
			Message: "explicit address invocation has no configured project schema; DTO fields are encoded without field type metadata",
		})
	}
	return InvocationPlan{
		Endpoint:  endpoint,
		Service:   input.Service,
		Method:    MethodSignature{Name: input.Method, ParamTypes: append([]string(nil), input.ParamTypes...)},
		Arguments: untypedArguments(input.OrderedArguments, input.ParamTypes),
		Timeout:   time.Duration(timeoutMS) * time.Millisecond,
		TimeoutMS: timeoutMS,
		RawResult: input.RawResult,
		Warnings:  warnings,
		Diagnostics: Diagnostics{
			Timing: map[string]int64{"planMs": time.Since(start).Milliseconds()},
			Resolution: map[string]interface{}{
				"service":        input.Service,
				"method":         input.Method,
				"endpointSource": "explicit-address",
			},
			Warnings: warnings,
		},
	}, nil
}

func (s *Service) ExecuteInvocation(ctx context.Context, plan InvocationPlan) InvocationExecution {
	out, err := direct.Invoke(ctx, directRequestFromPlan(plan))
	if err != nil {
		return InvocationExecution{
			OK:   false,
			Code: errorCode(err),
			Error: &ExecutionError{
				Message: err.Error(),
				Details: map[string]interface{}{
					"address":      plan.Endpoint.Address,
					"service":      plan.Service,
					"method":       plan.Method.Name,
					"rpcTimeoutMs": plan.TimeoutMS,
				},
			},
			Meta: map[string]interface{}{"runtime": "go"},
		}
	}
	data := map[string]interface{}{
		"result":      presentation.Flatten(out.AppResponse),
		"elapsedMs":   out.Elapsed.Milliseconds(),
		"diagnostics": out.Diagnostics,
	}
	if plan.RawResult {
		data["rawResult"] = out.AppResponse
	}
	return InvocationExecution{
		OK:   true,
		Code: CodeSuccess,
		Data: data,
		Meta: map[string]interface{}{"runtime": "go", "transport": "direct-bolt"},
	}
}

func directRequestFromPlan(plan InvocationPlan) direct.Request {
	return direct.Request{
		Address:     plan.Endpoint.Address,
		Service:     plan.Service,
		Method:      plan.Method.Name,
		ArgTypes:    plan.Method.ParamTypes,
		Args:        plan.DirectArgs(),
		Timeout:     plan.Timeout,
		AppName:     plan.Endpoint.AppName,
		Attachments: copyStringMap(plan.Endpoint.Attachments),
	}
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (s *Service) planArguments(ctx context.Context, projectName string, project appconfig.Project, service, method string, input InvocationInput) ([]javavalue.TypedValue, []string, []PlanWarning, error) {
	if input.HasOrderedArguments {
		return s.planOrderedArguments(ctx, projectName, project, service, method, input)
	}
	if input.NamedArguments == nil {
		return nil, nil, nil, &DomainError{Kind: ErrArgumentTypeMismatch, Message: "either orderedArguments or arguments is required"}
	}
	methodSchema, desc, err := s.resolveMethodDescription(ctx, projectName, project, service, method, input.ParamTypes)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(methodSchema.Parameters) == 1 {
		param := methodSchema.Parameters[0]
		if _, ok := input.NamedArguments[param.Name]; !ok {
			return []javavalue.TypedValue{typedValueForParam(input.NamedArguments, param.Type, methodSchema, desc)}, []string{rpcParamTypeForMethod(param.Type, methodSchema)}, nil, nil
		}
	}
	ordered := make([]javavalue.TypedValue, 0, len(methodSchema.Parameters))
	resolvedTypes := make([]string, 0, len(methodSchema.Parameters))
	for _, param := range methodSchema.Parameters {
		value, ok := input.NamedArguments[param.Name]
		if !ok {
			return nil, nil, nil, &DomainError{Kind: ErrArgumentTypeMismatch, Message: fmt.Sprintf("missing argument %q", param.Name), Details: map[string]interface{}{"parameter": param.Name}}
		}
		ordered = append(ordered, typedValueForParam(value, param.Type, methodSchema, desc))
		resolvedTypes = append(resolvedTypes, rpcParamTypeForMethod(param.Type, methodSchema))
	}
	return ordered, resolvedTypes, nil, nil
}

func (s *Service) planOrderedArguments(ctx context.Context, projectName string, project appconfig.Project, service, method string, input InvocationInput) ([]javavalue.TypedValue, []string, []PlanWarning, error) {
	paramTypes := append([]string(nil), input.ParamTypes...)
	var warnings []PlanWarning
	var methodSchema schema.Method
	var desc schema.Description
	hasSchema := false
	if len(paramTypes) == 0 {
		resolved, resolvedDesc, err := s.resolveMethodDescription(ctx, projectName, project, service, method, nil)
		if err != nil {
			return nil, nil, nil, err
		}
		if len(resolved.Parameters) != len(input.OrderedArguments) {
			return nil, nil, nil, &DomainError{Kind: ErrArgumentTypeMismatch, Message: fmt.Sprintf("resolved method has %d parameters, got %d arguments", len(resolved.Parameters), len(input.OrderedArguments)), Details: map[string]interface{}{"expected": len(resolved.Parameters), "actual": len(input.OrderedArguments)}}
		}
		methodSchema = resolved
		desc = resolvedDesc
		hasSchema = true
		paramTypes = rpcParamTypesForMethod(methodSchema)
	} else if needsSchemaAnnotation(paramTypes) {
		if resolved, resolvedDesc, err := s.resolveMethodDescription(ctx, projectName, project, service, method, paramTypes); err == nil {
			methodSchema = resolved
			desc = resolvedDesc
			hasSchema = true
		} else {
			warnings = append(warnings, PlanWarning{
				Code:    "SCHEMA_ANNOTATION_SKIPPED",
				Message: err.Error(),
			})
		}
	}
	if len(paramTypes) != len(input.OrderedArguments) {
		return nil, nil, warnings, &DomainError{Kind: ErrArgumentTypeMismatch, Message: fmt.Sprintf("paramTypes length (%d) does not match orderedArguments length (%d)", len(paramTypes), len(input.OrderedArguments)), Details: map[string]interface{}{"paramTypeCount": len(paramTypes), "argumentCount": len(input.OrderedArguments)}}
	}
	if hasSchema && len(methodSchema.Parameters) == len(input.OrderedArguments) {
		return typedArgumentsForMethod(input.OrderedArguments, methodSchema, desc), paramTypes, warnings, nil
	}
	return untypedArguments(input.OrderedArguments, paramTypes), paramTypes, warnings, nil
}

func (s *Service) resolveMethodDescription(ctx context.Context, projectName string, project appconfig.Project, service, method string, paramTypes []string) (schema.Method, schema.Description, error) {
	desc, err := s.sourceIndex().Describe(ctx, projectName, project, service, method)
	if err != nil {
		return schema.Method{}, schema.Description{}, err
	}
	var matches []schema.Method
	for _, candidate := range desc.Methods {
		if len(paramTypes) > 0 && !sameParamTypes(candidate, paramTypes) {
			continue
		}
		matches = append(matches, candidate)
	}
	if len(matches) == 0 {
		return schema.Method{}, schema.Description{}, &DomainError{Kind: ErrMethodNotFound, Message: fmt.Sprintf("method %s.%s not found for supplied paramTypes", service, method), Details: map[string]interface{}{"service": service, "method": method, "paramTypes": paramTypes}}
	}
	if len(matches) > 1 {
		return schema.Method{}, schema.Description{}, &DomainError{Kind: ErrMethodAmbiguous, Message: fmt.Sprintf("method %s.%s is overloaded; provide paramTypes. Candidates: %s", service, method, methodSignatures(matches)), Details: map[string]interface{}{"service": service, "method": method, "candidates": methodSignatures(matches)}}
	}
	return matches[0], desc, nil
}
