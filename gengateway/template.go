package gengateway

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/golang/glog"
	generator2 "github.com/golang/protobuf/protoc-gen-go/generator"
	"github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway/descriptor"
	"github.com/grpc-ecosystem/grpc-gateway/utilities"
)

type param struct {
	*descriptor.File
	Imports            []descriptor.GoPackage
	UseRequestContext  bool
	RegisterFuncSuffix string
	AllowPatchFeature  bool
}

type binding struct {
	*descriptor.Binding
	Registry          *descriptor.Registry
	AllowPatchFeature bool
}

// GetBodyFieldPath returns the binding body's fieldpath.
func (b binding) GetBodyFieldPath() string {
	if b.Body != nil && len(b.Body.FieldPath) != 0 {
		return b.Body.FieldPath.String()
	}
	return "*"
}

// HasQueryParam determines if the binding needs parameters in query string.
//
// It sometimes returns true even though actually the binding does not need.
// But it is not serious because it just results in a small amount of extra codes generated.
func (b binding) HasQueryParam() bool {
	if b.Body != nil && len(b.Body.FieldPath) == 0 {
		return false
	}
	fields := make(map[string]bool)
	for _, f := range b.Method.RequestType.Fields {
		fields[f.GetName()] = true
	}
	if b.Body != nil {
		delete(fields, b.Body.FieldPath.String())
	}
	for _, p := range b.PathParams {
		delete(fields, p.FieldPath.String())
	}
	return len(fields) > 0
}

func (b binding) QueryParamFilter() queryParamFilter {
	var seqs [][]string
	if b.Body != nil {
		seqs = append(seqs, strings.Split(b.Body.FieldPath.String(), "."))
	}
	for _, p := range b.PathParams {
		seqs = append(seqs, strings.Split(p.FieldPath.String(), "."))
	}
	return queryParamFilter{utilities.NewDoubleArray(seqs)}
}

// HasEnumPathParam returns true if the path parameter slice contains a parameter
// that maps to an enum proto field that is not repeated, if not false is returned.
func (b binding) HasEnumPathParam() bool {
	return b.hasEnumPathParam(false)
}

// HasRepeatedEnumPathParam returns true if the path parameter slice contains a parameter
// that maps to a repeated enum proto field, if not false is returned.
func (b binding) HasRepeatedEnumPathParam() bool {
	return b.hasEnumPathParam(true)
}

// hasEnumPathParam returns true if the path parameter slice contains a parameter
// that maps to a enum proto field and that the enum proto field is or isn't repeated
// based on the provided 'repeated' parameter.
func (b binding) hasEnumPathParam(repeated bool) bool {
	for _, p := range b.PathParams {
		if p.IsEnum() && p.IsRepeated() == repeated {
			return true
		}
	}
	return false
}

// LookupEnum looks up a enum type by path parameter.
func (b binding) LookupEnum(p descriptor.Parameter) *descriptor.Enum {
	e, err := b.Registry.LookupEnum("", p.Target.GetTypeName())
	if err != nil {
		return nil
	}
	return e
}

// FieldMaskField returns the golang-style name of the variable for a FieldMask, if there is exactly one of that type in
// the message. Otherwise, it returns an empty string.
func (b binding) FieldMaskField() string {
	var fieldMaskField *descriptor.Field
	for _, f := range b.Method.RequestType.Fields {
		if f.GetTypeName() == ".google.protobuf.FieldMask" {
			// if there is more than 1 FieldMask for this request, then return none
			if fieldMaskField != nil {
				return ""
			}
			fieldMaskField = f
		}
	}

	if fieldMaskField != nil {
		return generator2.CamelCase(fieldMaskField.GetName())
	}
	return ""
}

// queryParamFilter is a wrapper of utilities.DoubleArray which provides String() to output DoubleArray.Encoding in a stable and predictable format.
type queryParamFilter struct {
	*utilities.DoubleArray
}

func (f queryParamFilter) String() string {
	encodings := make([]string, len(f.Encoding))
	for str, enc := range f.Encoding {
		encodings[enc] = fmt.Sprintf("%q: %d", str, enc)
	}
	e := strings.Join(encodings, ", ")
	return fmt.Sprintf("&utilities.DoubleArray{Encoding: map[string]int{%s}, Base: %#v, Check: %#v}", e, f.Base, f.Check)
}

type trailerParams struct {
	Services           []*descriptor.Service
	UseRequestContext  bool
	RegisterFuncSuffix string
}

func applyTemplate(p param, reg *descriptor.Registry) (string, error) {
	w := bytes.NewBuffer(nil)
	if err := headerTemplate.Execute(w, p); err != nil {
		return "", err
	}
	var targetServices []*descriptor.Service
	for _, svc := range p.Services {
		var methodWithBindingsSeen bool
		svcName := strings.Title(*svc.Name)
		svc.Name = &svcName
		for _, meth := range svc.Methods {
			glog.V(2).Infof("Processing %s.%s", svc.GetName(), meth.GetName())
			methName := strings.Title(*meth.Name)
			meth.Name = &methName
			for _, b := range meth.Bindings {
				methodWithBindingsSeen = true
				if err := handlerTemplate.Execute(w, binding{
					Binding:           b,
					Registry:          reg,
					AllowPatchFeature: p.AllowPatchFeature,
				}); err != nil {
					return "", err
				}
			}
		}
		if methodWithBindingsSeen {
			targetServices = append(targetServices, svc)
		}
	}
	if len(targetServices) == 0 {
		return "", errNoTargetService
	}

	tp := trailerParams{
		Services:           targetServices,
		UseRequestContext:  p.UseRequestContext,
		RegisterFuncSuffix: p.RegisterFuncSuffix,
	}
	if err := trailerTemplate.Execute(w, tp); err != nil {
		return "", err
	}
	return w.String(), nil
}

var (
	headerTemplate = template.Must(template.New("header").Parse(`
// Code generated by protoc-gen-grpc-gateway. DO NOT EDIT.
// source: {{.GetName}}

/*
Package {{.GoPkg.Name}} is a reverse proxy.

It translates gRPC into RESTful JSON APIs.
*/
package {{.GoPkg.Name}}
import (
	{{range $i := .Imports}}{{if $i.Standard}}{{$i | printf "%s\n"}}{{end}}{{end}}

	{{range $i := .Imports}}{{if not $i.Standard}}{{$i | printf "%s\n"}}{{end}}{{end}}
)

var _ codes.Code
var _ io.Reader
var _ status.Status
var _ = runtime.String
var _ = utilities.NewDoubleArray
`))

	handlerTemplate = template.Must(template.New("handler").Parse(`
{{template "client-rpc-request-func" .}}
`))

	_ = template.Must(handlerTemplate.New("request-func-signature").Parse(strings.Replace(`
func request_{{.Method.Service.GetName}}_{{.Method.GetName}}_from_agent_{{.Index}}(ctx context.Context, client {{.Method.Service.GetName}}Client, c []byte) (proto.Message, runtime.ServerMetadata, error)
`, "\n", "", -1)))

	_ = template.Must(handlerTemplate.New("client-rpc-request-func").Parse(`
{{template "request-func-signature" .}} {
	var protoReq {{.Method.RequestType.GoType .Method.Service.File.GoPkg.Path}}
	var metadata runtime.ServerMetadata
	err := json.Unmarshal(c, &protoReq)
	if err != nil {
		return nil, metadata, err
	}

	msg, err := client.{{.Method.GetName}}(ctx, &protoReq)
	return msg, metadata, err
}`))

	trailerTemplate = template.Must(template.New("trailer").Parse(`
{{$UseRequestContext := .UseRequestContext}}
{{range $svc := .Services}}
func Create{{$svc.GetName}}Agent(endpoint string, opts []grpc.DialOption) (*grpcagent.Agent, error) {
	conn, err := grpc.Dial(endpoint, opts...)
	if err != nil {
		return nil, err
	}

	return grpcagent.CreateAgent(New{{$svc.GetName}}Client(conn), {{$svc.GetName}}ClientAgent{{$.RegisterFuncSuffix}}), nil
}

func {{$svc.GetName}}ClientAgent{{$.RegisterFuncSuffix}}(client interface{}, method string, c interface{}, md metadata.MD) (proto.Message, error) {
	jsonContent, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}

	_client, ok := client.({{$svc.GetName}}Client)
	if !ok {
		return nil, errors.New("{{$svc.GetName}}Client type assertion error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if md != nil {
		ctx = metadata.NewOutgoingContext(ctx, md)
	}

	{{range $m := $svc.Methods}}
	{{range $b := $m.Bindings}}
	if method == "{{$m.GetName}}" {
		resp, _, err := request_{{$svc.GetName}}_{{$m.GetName}}_from_agent_{{$b.Index}}(ctx, _client, jsonContent)
		return resp, err
	}
	{{end}}
	{{end}}

	return nil, errors.New("MethodNotFound")
}

{{end}}`))
)
