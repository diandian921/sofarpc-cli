package app

import (
	"context"
	"time"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
	"github.com/diandian921/sofarpc-mcp/internal/javavalue"
	"github.com/diandian921/sofarpc-mcp/internal/schema"
)

type ConfigStore interface {
	Load() (appconfig.Config, error)
}

type SourceIndex interface {
	Describe(ctx context.Context, projectName string, project appconfig.Project, service, method string) (schema.Description, error)
}

type DefaultConfigStore struct{}

func (DefaultConfigStore) Load() (appconfig.Config, error) {
	path, err := appconfig.DefaultPath()
	if err != nil {
		return appconfig.Config{}, err
	}
	return appconfig.Load(path)
}

type LocalSourceIndex struct{}

func (LocalSourceIndex) Describe(ctx context.Context, projectName string, project appconfig.Project, service, method string) (schema.Description, error) {
	if err := ctx.Err(); err != nil {
		return schema.Description{}, err
	}
	idx, err := schema.LoadOrBuildIndex(schema.Project{
		Name:            projectName,
		WorkspaceRoot:   project.WorkspaceRoot,
		ServicePrefixes: project.ServicePrefixes,
	})
	if err != nil {
		return schema.Description{}, err
	}
	if err := ctx.Err(); err != nil {
		return schema.Description{}, err
	}
	return schema.Describe(idx, service, method)
}

type Service struct {
	Store  ConfigStore
	Source SourceIndex
}

func New(store ConfigStore) *Service {
	if store == nil {
		store = DefaultConfigStore{}
	}
	return &Service{Store: store, Source: LocalSourceIndex{}}
}

func (s *Service) loadConfig() (appconfig.Config, error) {
	if s == nil || s.Store == nil {
		return DefaultConfigStore{}.Load()
	}
	return s.Store.Load()
}

func (s *Service) sourceIndex() SourceIndex {
	if s == nil || s.Source == nil {
		return LocalSourceIndex{}
	}
	return s.Source
}

type ProjectRef struct {
	Name string            `json:"name"`
	Info appconfig.Project `json:"info"`
}

type Endpoint struct {
	Server      string            `json:"server,omitempty"`
	Project     string            `json:"project,omitempty"`
	Address     string            `json:"address"`
	Protocol    string            `json:"protocol"`
	TimeoutMS   int               `json:"timeoutMs"`
	AppName     string            `json:"appName"`
	Attachments map[string]string `json:"attachments"`
}

type MethodSignature struct {
	Name       string   `json:"name"`
	ParamTypes []string `json:"paramTypes"`
}

type PlanWarning struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

type Diagnostics struct {
	Timing     map[string]int64       `json:"timing,omitempty"`
	Resolution map[string]interface{} `json:"resolution,omitempty"`
	Warnings   []PlanWarning          `json:"warnings,omitempty"`
}

type ResolveInput struct {
	Project   string
	Server    string
	Address   string
	Service   string
	TimeoutMS int
}

type ResolveResult struct {
	Project     ProjectRef               `json:"project"`
	Server      string                   `json:"server,omitempty"`
	Endpoint    *Endpoint                `json:"endpoint,omitempty"`
	Servers     []map[string]interface{} `json:"servers,omitempty"`
	Network     string                   `json:"network"`
	Diagnostics Diagnostics              `json:"diagnostics,omitempty"`
}

type InvocationInput struct {
	Project             string
	Server              string
	Address             string
	Protocol            string
	AppName             string
	Service             string
	Method              string
	ParamTypes          []string
	OrderedArguments    []interface{}
	HasOrderedArguments bool
	NamedArguments      map[string]interface{}
	TimeoutMS           int
	RawResult           bool
}

type InvocationPlan struct {
	Project     ProjectRef             `json:"project"`
	Server      string                 `json:"server"`
	Endpoint    Endpoint               `json:"endpoint"`
	Service     string                 `json:"service"`
	Method      MethodSignature        `json:"method"`
	Arguments   []javavalue.TypedValue `json:"arguments"`
	Timeout     time.Duration          `json:"-"`
	TimeoutMS   int                    `json:"timeoutMs"`
	RawResult   bool                   `json:"rawResult,omitempty"`
	Warnings    []PlanWarning          `json:"warnings,omitempty"`
	Diagnostics Diagnostics            `json:"diagnostics,omitempty"`
}

type InvocationExecution struct {
	OK    bool                   `json:"ok"`
	Code  string                 `json:"code"`
	Data  map[string]interface{} `json:"data,omitempty"`
	Error *ExecutionError        `json:"error,omitempty"`
	Meta  map[string]interface{} `json:"meta,omitempty"`
}

type ExecutionError struct {
	Message string                 `json:"message"`
	Cause   string                 `json:"cause,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
}

func (p InvocationPlan) DirectArgs() []interface{} {
	out := make([]interface{}, len(p.Arguments))
	for i := range p.Arguments {
		out[i] = p.Arguments[i]
	}
	return out
}

func (p InvocationPlan) Display() map[string]interface{} {
	args := make([]interface{}, len(p.Arguments))
	for i, arg := range p.Arguments {
		args[i] = arg.Display()
	}
	return map[string]interface{}{
		"server":           p.Server,
		"project":          p.Project.Name,
		"projectInfo":      p.Project.Info,
		"endpoint":         p.Endpoint,
		"service":          p.Service,
		"method":           p.Method.Name,
		"paramTypes":       p.Method.ParamTypes,
		"argTypes":         p.Method.ParamTypes,
		"orderedArguments": args,
		"timeoutMs":        p.TimeoutMS,
		"rawResult":        p.RawResult,
		"warnings":         p.Warnings,
		"diagnostics":      p.Diagnostics,
	}
}
