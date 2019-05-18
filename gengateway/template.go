package gengateway

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

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

type meths struct {
	*descriptor.Method
	Registry          *descriptor.Registry
	AllowPatchFeature bool
}

func applyTemplate(p param, reg *descriptor.Registry) (string, error) {
	w := bytes.NewBuffer(nil)
	if err := headerTemplate.Execute(w, p); err != nil {
		return "", err
	}
	var targetServices []*descriptor.Service
	for _, svc := range p.Services {
		svcName := strings.Title(*svc.Name)
		svc.Name = &svcName
		for _, meth := range svc.Methods {
			methName := strings.Title(*meth.Name)
			meth.Name = &methName

			if err := handlerTemplate.Execute(w, meths{
				Method:            meth,
				Registry:          reg,
				AllowPatchFeature: p.AllowPatchFeature,
			}); err != nil {
				return "", err
			}
		}
		targetServices = append(targetServices, svc)
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
{{if or .GetClientStreaming .GetServerStreaming}}
{{template "client-dummy-request-func" .}}
{{else}}
{{template "client-rpc-request-func" .}}
{{end}}
`))

	_ = template.Must(handlerTemplate.New("request-func-signature").Parse(strings.Replace(`
func request_{{.Service.GetName}}_{{.GetName}}_from_agent(ctx context.Context, client {{.Service.GetName}}Client, c []byte) (proto.Message, runtime.ServerMetadata, error)
`, "\n", "", -1)))

	_ = template.Must(handlerTemplate.New("client-dummy-request-func").Parse(`
{{template "request-func-signature" .}} {
	var protoReq {{.RequestType.GoType .Service.File.GoPkg.Path}}
	var metadata runtime.ServerMetadata
	err := json.Unmarshal(c, &protoReq)
	if err != nil {
		return nil, metadata, err
	}

	return nil, metadata, err
}`))
	_ = template.Must(handlerTemplate.New("client-rpc-request-func").Parse(`
{{template "request-func-signature" .}} {
	var protoReq {{.RequestType.GoType .Service.File.GoPkg.Path}}
	var metadata runtime.ServerMetadata
	err := json.Unmarshal(c, &protoReq)
	if err != nil {
		return nil, metadata, err
	}

	msg, err := client.{{.GetName}}(ctx, &protoReq)
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
	if method == "{{$m.GetName}}" {
		resp, _, err := request_{{$svc.GetName}}_{{$m.GetName}}_from_agent(ctx, _client, jsonContent)
		return resp, err
	}
	{{end}}

	return nil, errors.New("MethodNotFound")
}

{{end}}`))
)
