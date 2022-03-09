package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
//	"reflect"
	gnmi_pb "github.com/Juniper/jtimon/gnmi/gnmi"
	gnmi_dialout_pb "github.com/Juniper/jtimon/gnmidialout"
	grpc "google.golang.org/grpc"
	peer "google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/codes"
)

//File holds filename to open
type File struct {
        Filename string
}

//Write creates and/or appends to a file
func (f *File) Write(message []byte) {
        file, err := os.OpenFile(f.Filename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
        if err != nil {
                log.Fatalf("failed opening file: %s", err)
        }
        defer file.Close()

        _, err = file.Write(message)
        if err != nil {
                log.Fatalf("failed writing to file: %s", err)
        }
}

type grpcLocalServer struct {
        gnmi_dialout_pb.UnimplementedSubscriberServer
	jctx *JCtx
	// nothing yet
}

type dummyPeerType struct {
	// nothing yet
}

func (d *dummyPeerType) String() string {
	return "Unknown addr"
}

func (d *dummyPeerType) Network() string {
	return "Unknown net"
}

var dummyPeer dummyPeerType

func decrypt(data *gnmi_pb.SubscribeResponse) {
//	ProtoItem := new(SubscribeResponse)
//	err := proto.Unmarshal(data, ProtoItem)
//	if err != nil {
//	    log.Fatal(err)
//	}
        //go printer(data)
//	var jsonpbObject jsonpb.Marshaler
//	jsonString, err := jsonpbObject.MarshalToString(data)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	buf := new(bytes.Buffer)
//       json.Indent(buf, []byte(jsonString), "", "  ")
//	go printer(buf.Bytes())

}

func printer(data []byte) {
    f := File{Filename: "output.log"}
    f.Write(data)
    fmt.Printf("%s", data)
}


func (s *grpcLocalServer) DialOutSubscriber(stream gnmi_dialout_pb.Subscriber_DialOutSubscriberServer) error {

	var endpoint *peer.Peer
	var ok bool
	datach := make(chan SubErrorCode)

	fmt.Printf("Received connection request !\n")

	if endpoint, ok = peer.FromContext(stream.Context()); !ok {
		endpoint = &peer.Peer{
			Addr: &dummyPeer,
		}
	}
	fmt.Printf("Receiving dialout stream from %s!\n", endpoint.Addr.String())

	for {
		var (
			rsp *gnmi_pb.SubscribeResponse
			err error
			hostname = s.jctx.config.Host + ":" + strconv.Itoa(s.jctx.config.Port)
		)
		rsp, err = stream.Recv()

		if err == io.EOF {
			printSummary(s.jctx)
			jLog(s.jctx, fmt.Sprintf("gNMI host: %v, received eof", hostname))
			datach <- SubRcConnRetry
			return nil
		}

		if err != nil {
			jLog(s.jctx, fmt.Sprintf("gNMI host: %v, receive response failed: %v", hostname, err))
			sc, _ := status.FromError(err)

			/*
			* Unavailable is just a cover-up for JUNOS, ideally the device is expected to return:
			*   1. Unimplemented if RPC is not available yet
			*   2. InvalidArgument is RPC is not able to honour the input
			*/
			if sc.Code() == codes.Unimplemented || sc.Code() == codes.InvalidArgument || sc.Code() == codes.Unavailable {
				datach <- SubRcRPCFailedNoRetry
				return nil
			}

			datach <- SubRcConnRetry
			return nil
		}
		if *noppgoroutines {
			err = gnmiHandleResponse(s.jctx, rsp)
			if err != nil && strings.Contains(err.Error(), gGnmiJtimonIgnoreErrorSubstr) {
				jLog(s.jctx, fmt.Sprintf("gNMI host: %v, parsing response failed: %v", hostname, err))
				continue
			}
		} else {
			go func() {
				err = gnmiHandleResponse(s.jctx, rsp)
				if err != nil && strings.Contains(err.Error(), gGnmiJtimonIgnoreErrorSubstr) {
					jLog(s.jctx, fmt.Sprintf("gNMI host: %v, parsing response failed: %v", hostname, err))
				}
			}()
		}
	}
}


func newServer(jctx *JCtx) *grpcLocalServer {
	s := &grpcLocalServer{jctx: jctx}
	return s
}

type EdgeServerStream struct {
	grpc.ServerStream
}

// Interceptor for debugging, disabled or nop
func (e *EdgeServerStream) RecvMsg(m interface{}) error {
	//log.Printf("intercepted server stream message, type: %s", reflect.TypeOf(m).String())
	if err := e.ServerStream.RecvMsg(m); err != nil {
		return err
	}
	return nil

}

// Interceptor for debugging, disabled or nop
func StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		wrapper := &EdgeServerStream {
			ServerStream: ss,
		}
		return handler(srv, wrapper)
	}
}

func start_dialout_server(jctx *JCtx, opts []grpc.ServerOption) {
	port := strconv.Itoa(jctx.config.Port)
	fmt.Printf("Starting gRPC Dialout Collector, Host: %s, Port: %s.\n", jctx.config.Host, port)

	portaddr := jctx.config.Host
	portaddr += ":"
	portaddr += port

	lis, err := net.Listen("tcp", portaddr)
	if (err != nil) {
	fmt.Printf("Socket listening failed: %s\n", err)
		return
	}
	fmt.Printf("gRPC Server starting at: %s \n", portaddr)
	opts = append(opts, grpc.StreamInterceptor(StreamServerInterceptor()))
	grpcServer := grpc.NewServer(opts...)
	gnmi_dialout_pb.RegisterSubscriberServer(grpcServer, newServer(jctx))
	grpcServer.Serve(lis)
	fmt.Printf("Exiting server...\n")
}
