// Package tools is the SofaRPC business-tool layer of the MCP server. Each tool is
// registered on the official go-sdk server (see the *_sdk.go files) with a
// hand-written schema, decodes its typed arguments, and calls the app / schema /
// appconfig packages, returning the unified app.Result envelope.
package tools

import "encoding/json"

// errorSchema is the shared error sub-object of the app.Result envelope.
var errorSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "message": {"type": "string"},
    "cause": {"type": "string"},
    "nextTool": {"type": "string"},
    "recovery": {"type": "string"},
    "details": {"type": "object"}
  }
}`)

// resultOutputSchemaWithData builds the unified app.Result envelope schema with the
// given per-tool schema spliced in as the `data` sub-schema. The envelope stays
// permissive (no required, additionalProperties allowed) so the one schema validates
// both success (data present) and failure (data absent, error present) results; raw
// Server.AddTool does not validate output, so this only documents the contract for
// clients/LLMs (and the guard tests).
func resultOutputSchemaWithData(dataSchema json.RawMessage) json.RawMessage {
	envelope := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ok":        map[string]any{"type": "boolean"},
			"code":      map[string]any{"type": "string"},
			"requestId": map[string]any{"type": "string"},
			"data":      dataSchema,
			"error":     errorSchema,
			"meta":      map[string]any{"type": "object"},
		},
	}
	out, err := json.Marshal(envelope)
	if err != nil {
		// dataSchema is always a compile-time literal, so marshal cannot fail here.
		panic(err)
	}
	return out
}

// Per-tool data schemas describe the real top-level shape of each tool's
// app.Result.data. Nested objects stay open (no deep required) so a richer endpoint,
// plan, or — for invoke — an arbitrary decoded Java result tree does not break the
// contract. resolve/describe pre-declare `candidates` so the Sprint 2 candidate
// enhancement does not have to revise these schemas.
var (
	resolveDataSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "project": {"type": "string"},
    "projectInfo": {"type": "object"},
    "server": {"type": "string"},
    "endpoint": {"type": "object"},
    "servers": {"type": "array"},
    "network": {"type": "string"},
    "diagnostics": {"type": "object"},
    "candidates": {"type": "array"}
  }
}`)

	probeDataSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "address": {"type": "string"},
    "service": {"type": "string"},
    "reachable": {"type": "boolean"},
    "elapsedMs": {"type": "number"},
    "diagnostics": {"type": "object"}
  }
}`)

	describeDataSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "project": {"type": "string"},
    "query": {"type": "string"},
    "candidates": {"type": "array"},
    "description": {"type": "object"}
  }
}`)

	doctorDataSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "checks": {"type": "array", "items": {"type": "object"}}
  }
}`)

	configListDataSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "configPath": {"type": "string"},
    "projectFilter": {"type": "string"},
    "projects": {"type": "array"},
    "servers": {"type": "array"},
    "writeEnabled": {"type": "boolean"}
  }
}`)

	invokeDataSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "result": {},
    "rawResult": {},
    "assertions": {"type": "array"},
    "warnings": {"type": "array"},
    "diagnostics": {"type": "object"},
    "elapsedMs": {"type": "number"}
  }
}`)

	invokePlanDataSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "dryRun": {"type": "boolean"},
    "plan": {"type": "object"}
  }
}`)

	configSaveProjectDataSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string"},
    "project": {"type": "object"}
  }
}`)

	configSaveServerDataSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string"},
    "server": {"type": "object"}
  }
}`)

	configRemoveDataSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "removed": {"type": "string"}
  }
}`)
)

// Per-tool output schemas: the unified envelope wrapping each tool's data schema.
var (
	resolveOutputSchema           = resultOutputSchemaWithData(resolveDataSchema)
	probeOutputSchema             = resultOutputSchemaWithData(probeDataSchema)
	describeOutputSchema          = resultOutputSchemaWithData(describeDataSchema)
	doctorOutputSchema            = resultOutputSchemaWithData(doctorDataSchema)
	configListOutputSchema        = resultOutputSchemaWithData(configListDataSchema)
	invokeOutputSchema            = resultOutputSchemaWithData(invokeDataSchema)
	invokePlanOutputSchema        = resultOutputSchemaWithData(invokePlanDataSchema)
	configSaveProjectOutputSchema = resultOutputSchemaWithData(configSaveProjectDataSchema)
	configSaveServerOutputSchema  = resultOutputSchemaWithData(configSaveServerDataSchema)
	configRemoveOutputSchema      = resultOutputSchemaWithData(configRemoveDataSchema)
)
