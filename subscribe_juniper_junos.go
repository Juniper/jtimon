package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"encoding/json"
	"os"
	"syscall"

	auth_pb "github.com/Juniper/jtimon/authentication"
	na_pb "github.com/Juniper/jtimon/telemetry"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// SubErrorCode to define the type of errors
type SubErrorCode int

// Error Codes for Subscribe Send routines
const (
	SubRcConnRetry = iota
	SubRcSighupRestart
	SubRcSighupNoRestart
	SubRcRPCFailedNoRetry
)

func handleOnePacket(ocData *na_pb.OpenConfigData, jctx *JCtx) {
	updateStats(jctx, ocData, true)

	// Struct to hold the data for JSON output
	logData := make(map[string]interface{})

	// String format logging
	s := ""

	if *print || (IsVerboseLogging(jctx) && !*print) || (isInternalJtimonLogging(jctx)) {
		// Add data to both the string and map
		systemId := ocData.SystemId
		componentId := ocData.ComponentId
		subComponentId := ocData.SubComponentId
		path := ocData.Path
		sequenceNumber := ocData.SequenceNumber
		timestamp := ocData.Timestamp
		syncResponse := ocData.SyncResponse

		// String logging
		s += fmt.Sprintf("system_id: %s\n", systemId)
		s += fmt.Sprintf("component_id: %d\n", componentId)
		s += fmt.Sprintf("sub_component_id: %d\n", subComponentId)
		s += fmt.Sprintf("path: %s\n", path)
		s += fmt.Sprintf("sequence_number: %d\n", sequenceNumber)
		s += fmt.Sprintf("timestamp: %d\n", timestamp)
		s += fmt.Sprintf("sync_response: %v\n", syncResponse)
		if syncResponse {
			s += "Received sync_response\n"
		}

		// Add data to map for JSON
		logData["system_id"] = systemId
		logData["component_id"] = componentId
		logData["sub_component_id"] = subComponentId
		logData["path"] = path
		logData["sequence_number"] = sequenceNumber
		logData["timestamp"] = timestamp
		logData["sync_response"] = syncResponse

		del := ocData.GetDelete()
		deletePaths := []string{}
		for _, d := range del {
			path := d.GetPath()
			s += fmt.Sprintf("Delete: %s\n", path)
			deletePaths = append(deletePaths, path)
		}
		logData["delete_paths"] = deletePaths
	}

	prefixSeen := false
	kvData := []map[string]interface{}{}

	for _, kv := range ocData.Kv {
		updateStatsKV(jctx, true, 1)

		kvItem := make(map[string]interface{})
		kvItem["key"] = kv.Key

		if *print || (IsVerboseLogging(jctx) && !*print) || (isInternalJtimonLogging(jctx)) {
			s += fmt.Sprintf("  key: %s\n", kv.Key)
			switch value := kv.Value.(type) {
			case *na_pb.KeyValue_DoubleValue:
				s += fmt.Sprintf("  double_value: %v\n", value.DoubleValue)
				kvItem["double_value"] = value.DoubleValue
			case *na_pb.KeyValue_IntValue:
				s += fmt.Sprintf("  int_value: %d\n", value.IntValue)
				kvItem["int_value"] = value.IntValue
			case *na_pb.KeyValue_UintValue:
				s += fmt.Sprintf("  uint_value: %d\n", value.UintValue)
				kvItem["uint_value"] = value.UintValue
			case *na_pb.KeyValue_SintValue:
				s += fmt.Sprintf("  sint_value: %d\n", value.SintValue)
				kvItem["sint_value"] = value.SintValue
			case *na_pb.KeyValue_BoolValue:
				s += fmt.Sprintf("  bool_value: %v\n", value.BoolValue)
				kvItem["bool_value"] = value.BoolValue
			case *na_pb.KeyValue_StrValue:
				s += fmt.Sprintf("  str_value: %s\n", value.StrValue)
				kvItem["str_value"] = value.StrValue
			case *na_pb.KeyValue_BytesValue:
				s += fmt.Sprintf("  bytes_value: %s\n", value.BytesValue)
				kvItem["bytes_value"] = value.BytesValue
			case *na_pb.KeyValue_LeaflistValue:
				s += fmt.Sprintf("  leaf_list_value: %s\n", value.LeaflistValue)
				e := kv.GetLeaflistValue().Element
				elements := []string{}
				for _, elem := range e {
					if llStrValue := elem.GetLeaflistStrValue(); llStrValue != "" {
						s += fmt.Sprintf("  leaf_list_value(element): %s\n", llStrValue)
						elements = append(elements, llStrValue)
					}
				}
				kvItem["leaf_list_elements"] = elements
			default:
				s += fmt.Sprintf("  default: %v\n", value)
				kvItem["default"] = value
			}

			kvData = append(kvData, kvItem)
		}

		if kv.Key == "__prefix__" {
			prefixSeen = true
		} else if !strings.HasPrefix(kv.Key, "__") {
			if !prefixSeen && !strings.HasPrefix(kv.Key, "/") {
				if *prefixCheck {
					s += fmt.Sprintf("Missing prefix for sensor: %s\n", ocData.Path)
				}
			}
		}
	}

	logData["kv"] = kvData

	if s != "" && (*print || (IsVerboseLogging(jctx) && !*print)) {
		jLog(jctx, s)
	}

	if isInternalJtimonLogging(jctx) {
		if *outJSON {
			jsonOutput, err := json.MarshalIndent(logData, "", "  ")
			if err != nil {
				jLog(jctx, fmt.Sprintf("Error marshaling to JSON: %v", err))
				return
			}
			jLogInternalJtimonForPreGnmi(jctx, nil, string(jsonOutput))
		} else {
			if s != "" {
				jLogInternalJtimonForPreGnmi(jctx, nil, s)
			}
		}
	}
}

// subSendAndReceive handles the following
//   - Opens up a stream for receiving the telemetry data
//   - Handles SIGHUP by terminating the current stream and requests the
//     caller to restart the streaming by setting the corresponding return
//     code
//   - In case of an error, Set the error code to restart the connection.
func subSendAndReceive(conn *grpc.ClientConn, jctx *JCtx,
	subReqM na_pb.SubscriptionRequest) SubErrorCode {

	var ctx context.Context
	c := na_pb.NewOpenConfigTelemetryClient(conn)
	if jctx.config.Meta {
		md := metadata.New(map[string]string{"username": jctx.config.User, "password": jctx.config.Password})
		ctx = metadata.NewOutgoingContext(context.Background(), md)
	} else {
		ctx = context.Background()
	}
	stream, err := c.TelemetrySubscribe(ctx, &subReqM)

	if err != nil {
		return SubRcConnRetry
	}

	hdr, errh := stream.Header()
	if errh != nil {
		jLog(jctx, fmt.Sprintf("Failed to get header for stream: %v", errh))
	}

	jLog(jctx, fmt.Sprintf("gRPC headers from host %s:%d\n", jctx.config.Host, jctx.config.Port))
	for k, v := range hdr {
		jLog(jctx, fmt.Sprintf("  %s: %s", k, v))
	}

	datach := make(chan struct{})

	go func() {
		// Go Routine which actually starts the streaming connection and receives the data
		jLog(jctx, fmt.Sprintf("Receiving telemetry data from %s:%d\n", jctx.config.Host, jctx.config.Port))

		for {
			ocData, err := stream.Recv()
			if err == io.EOF {
				printSummary(jctx)
				datach <- struct{}{}
				return
			}
			if err != nil {
				jLog(jctx, fmt.Sprintf("%v.TelemetrySubscribe(_) = _, %v", conn, err))
				datach <- struct{}{}
				return
			}

			if *genTestData {
				if ocDataM, err := proto.Marshal(ocData); err == nil {
					generateTestData(jctx, ocDataM)
				} else {
					jLog(jctx, fmt.Sprintf("%v", err))
				}
			}

			rtime := time.Now()
			if *outJSON {
				if b, err := json.MarshalIndent(ocData, "", "  "); err == nil {
					jLog(jctx, fmt.Sprintf("%s\n", b))
				}
			}

			if *print || *statsHandler || IsVerboseLogging(jctx) || isInternalJtimonLogging(jctx) {
				handleOnePacket(ocData, jctx)
			}

			// to influxdb
			if *noppgoroutines {
				addIDB(ocData, jctx, rtime)
			} else {
				go addIDB(ocData, jctx, rtime)
			}

			// to prometheus
			if *prom {
				if *noppgoroutines {
					addPrometheus(ocData, jctx)
				} else {
					go addPrometheus(ocData, jctx)
				}
			}
			// to kafka
			if *noppgoroutines {
				addKafka(ocData, jctx, rtime)
			} else {
				go addKafka(ocData, jctx, rtime)
			}
		}
	}()
	for {
		select {
		case s := <-jctx.control:
			switch s {
			case syscall.SIGHUP:
				// config has been updated restart the streaming
				return SubRcSighupRestart
			case os.Interrupt:
				// we are done
				return SubRcSighupNoRestart
			}
		case <-datach:
			// data is not received, retry the connection
			return SubRcConnRetry
		}
	}
}

// subscribe routine constructs the subscription paths and calls
// the function to start the streaming connection.
//
// In case of SIGHUP, the paths are formed again and streaming
// is restarted.
func subscribeJunos(conn *grpc.ClientConn, jctx *JCtx, cfg Config, paths []PathsConfig) SubErrorCode {
	var subReqM na_pb.SubscriptionRequest
	var additionalConfigM na_pb.SubscriptionAdditionalConfig

	//cfg := &jctx.config
	for i := range paths {
		var pathM na_pb.Path
		pathM.Path = paths[i].Path
		pathM.SampleFrequency = uint32(paths[i].Freq)
		subReqM.PathList = append(subReqM.PathList, &pathM)
	}
	additionalConfigM.NeedEos = jctx.config.EOS
	// Override EOS if InternalJtimon is configured
	if isInternalJtimonLogging(jctx) {
		additionalConfigM.NeedEos = jctx.config.InternalJtimon.PreGnmiEOS
	}
	subReqM.AdditionalConfig = &additionalConfigM

	return subSendAndReceive(conn, jctx, subReqM)
}

func loginCheckJunos(jctx *JCtx, conn *grpc.ClientConn) error {
	if jctx.config.User != "" && jctx.config.Password != "" {
		user := jctx.config.User
		pass := jctx.config.Password
		if !jctx.config.Meta {
			lc := auth_pb.NewLoginClient(conn)
			dat, err := lc.LoginCheck(context.Background(),
				&auth_pb.LoginRequest{UserName: user,
					Password: pass, ClientId: jctx.config.CID})
			if err != nil {
				return fmt.Errorf("[%s] Could not login: %v", jctx.config.Host, err)
			}
			if !dat.Result {
				return fmt.Errorf("[%s] LoginCheck failed", jctx.config.Host)
			}
		}
	}
	return nil
}
