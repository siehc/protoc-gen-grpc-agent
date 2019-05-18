# protoc-gen-grpc-agent
A plugin of protobuf compiler [protoc](https://github.com/protocolbuffers/protobuf)

Base on [grpc-gateway](https://github.com/grpc-ecosystem/grpc-gateway)

## Installation
```sh
go get -u github.com/siehc/protoc-gen-grpc-agent
go get -u github.com/golang/protobuf/protoc-gen-go
```

This will place two binaries in your `$GOBIN`;

* `protoc-gen-grpc-agent`
* `protoc-gen-go`

Make sure that your `$GOBIN` is in your `$PATH`.

## Usage
1. Generate GRPC stub and agent

   ```sh
   protoc -I/usr/local/include -I. \
     -I$GOPATH/src \
     -I$GOPATH/src/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis \
     --go_out=plugins=grpc:. \
     --grpc-agent_out=logtostderr=true:. \
     path/to/your_service.proto
   ```

   It will generate a stub file `path/to/your_service.pb.go` and a agent file `path/to/your_service.pb.agent.go`.

1. Use agent

   Now you need to write an entrypoint of the proxy server.
   ```go
   package main

   import (
     "flag"
     "fmt"

     "github.com/golang/glog"
     "google.golang.org/grpc"

     gw "path/to/your_service_package"
   )

   var (
     echoEndpoint = flag.String("echo_endpoint", "localhost:9090", "endpoint of YourService")
   )

   func main() {
     opts := []grpc.DialOption{grpc.WithInsecure()}
     agent, err := gw.CreateYourServiceAgent(*echoEndpoint, opts)
     if err != nil {
       return err
     }

     resp, err := agent.Request("yourmethodname", yourdata)
     if err != nil {
       return err
     }

     fmt.Println(resp)
   }
   ```