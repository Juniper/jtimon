package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	gnmi "github.com/Juniper/jtimon/gnmi/gnmi"
	gnmi_ext1 "github.com/Juniper/jtimon/gnmi/gnmi_ext"
	gnmi_juniper_header "github.com/Juniper/jtimon/gnmi/gnmi_juniper_header"
	gnmi_juniper_header_ext "github.com/Juniper/jtimon/gnmi/gnmi_juniper_header_ext"
	"github.com/golang/protobuf/proto"
	google_protobuf "github.com/golang/protobuf/ptypes/any"
)

func TestConvToFloatForPrometheus(t *testing.T) {
	tests := []struct {
		name   string
		input  interface{}
		err    bool
		output float64
	}{
		{
			name:  "int",
			input: 100,
			err:   true,
		},
		{
			name:  "uint",
			input: 100,
			err:   true,
		},
		{
			name:   "int64",
			input:  int64(100),
			err:    false,
			output: float64(100),
		},
		{
			name:   "bool",
			input:  true,
			err:    false,
			output: float64(1),
		},
		{
			name:   "string",
			input:  "100",
			err:    false,
			output: float64(100),
		},
		{
			name:  "string-err",
			input: "helloe",
			err:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output, err := convToFloatForPrometheus(test.input)
			if !test.err {
				if err != nil || !reflect.DeepEqual(test.output, output) {
					var errMsg string
					errMsg = fmt.Sprintf("\nexpected:%v\nGot:%v", test.output, output)
					t.Errorf(errMsg)
				}
			}

			if test.err {
				if err == nil && reflect.DeepEqual(test.output, output) {
					var errMsg string
					errMsg = fmt.Sprintf("\nexpected:%v\nGot:%v", test.output, output)
					t.Errorf(errMsg)
				}
			}
		})
	}
}

func TestGnmiHandleResponse(t *testing.T) {
	*noppgoroutines = true
	*prom = true
	gGnmiUnitTestCoverage = true

	var hdrInputExt = gnmi_juniper_header_ext.GnmiJuniperTelemetryHeaderExtension{
		SystemId: "my-device", ComponentId: 65535, SubComponentId: 0,
		SensorName: "sensor_1", SequenceNumber: 1, SubscribedPath: "/interfaces/",
		StreamedPath: "/interfaces/", Component: "mib2d",
	}

	hdrInputExtBytes, err := proto.Marshal(&hdrInputExt)
	if err != nil {
		t.Errorf("Error marshalling header for ext case: %v", err)
	}

	var hdrInputXpath = gnmi_juniper_header.GnmiJuniperTelemetryHeader{
		SystemId: "my-device", ComponentId: 65535, SubComponentId: 0,
		Path: "sensor_1:/interfaces/:/interfaces/:mib2d", SequenceNumber: 1,
	}

	hdrInputXpathBytes, err := proto.Marshal(&hdrInputXpath)
	if err != nil {
		t.Errorf("Error marshalling header for xpath case: %v", err)
	}

	var hdrInputExtIsync = gnmi_juniper_header_ext.GnmiJuniperTelemetryHeaderExtension{
		SystemId: "my-device", ComponentId: 65535, SubComponentId: 0,
		SensorName: "sensor_1", SequenceNumber: gGnmiJuniperIsyncSeqNumBegin, SubscribedPath: "/interfaces/",
		StreamedPath: "/interfaces/", Component: "mib2d",
	}

	hdrInputExtIsyncBytes, err := proto.Marshal(&hdrInputExtIsync)
	if err != nil {
		t.Errorf("Error marshalling header for ext isync case: %v", err)
	}

	var hdrInputXpathIsync = gnmi_juniper_header.GnmiJuniperTelemetryHeader{
		SystemId: "my-device", ComponentId: 65535, SubComponentId: 0,
		Path: "sensor_1:/interfaces/:/interfaces/:mib2d", SequenceNumber: gGnmiJuniperIsyncSeqNumEnd,
	}

	hdrInputXpathIsyncBytes, err := proto.Marshal(&hdrInputXpathIsync)
	if err != nil {
		t.Errorf("Error marshalling header for xpath isync case: %v", err)
	}

	tests := []struct {
		name string
		jctx *JCtx
		rsp  *gnmi.SubscribeResponse
		err  bool
	}{
		{
			name: "rsp-valid-sync",
			err:  false,
			jctx: &JCtx{
				config: Config{
					Host: "127.0.0.1",
					Port: 32767,
					Log: LogConfig{
						Verbose: true,
					},
				},
			},
			rsp: &gnmi.SubscribeResponse{
				Response: &gnmi.SubscribeResponse_SyncResponse{
					SyncResponse: true,
				},
			},
		},
		{
			name: "rsp-valid-updates",
			err:  false,
			jctx: &JCtx{
				config: Config{
					Host: "127.0.0.1",
					Port: 32767,
					Log: LogConfig{
						Verbose: true,
					},
				},
			},
			rsp: &gnmi.SubscribeResponse{
				Extension: []*gnmi_ext1.Extension{
					{
						Ext: &gnmi_ext1.Extension_RegisteredExt{
							RegisteredExt: &gnmi_ext1.RegisteredExtension{
								Id:  gnmi_ext1.ExtensionID_EID_JUNIPER_TELEMETRY_HEADER,
								Msg: hdrInputExtBytes,
							},
						},
					},
				},
				Response: &gnmi.SubscribeResponse_Update{
					Update: &gnmi.Notification{
						Timestamp: 1589476296083000000,
						Prefix: &gnmi.Path{
							Origin: "",
							Elem: []*gnmi.PathElem{
								{Name: "interfaces"},
								{Name: "interface", Key: map[string]string{"k1": "foo"}},
								{Name: "subinterfaces"},
								{Name: "subinterface", Key: map[string]string{"k1": "foo1", "k2": "bar1"}},
							},
						},
						Update: []*gnmi.Update{
							{
								Path: &gnmi.Path{
									Origin: "",
									Elem: []*gnmi.PathElem{
										{Name: "state"},
										{Name: "description"},
									},
								},
								Val: &gnmi.TypedValue{
									Value: &gnmi.TypedValue_StringVal{StringVal: "Hello"},
								},
							},
							{
								Path: &gnmi.Path{
									Origin: "",
									Elem: []*gnmi.PathElem{
										{Name: "state"},
										{Name: "mtu"},
									},
								},
								Val: &gnmi.TypedValue{
									Value: &gnmi.TypedValue_IntVal{IntVal: 1500},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "rsp-valid-deletes",
			err:  false,
			jctx: &JCtx{
				config: Config{
					Host: "127.0.0.1",
					Port: 32767,
					Log: LogConfig{
						Verbose: true,
					},
				},
			},
			rsp: &gnmi.SubscribeResponse{
				Response: &gnmi.SubscribeResponse_Update{
					Update: &gnmi.Notification{
						Timestamp: 1589476296083000000,
						Prefix: &gnmi.Path{
							Origin: "",
							Elem: []*gnmi.PathElem{
								{Name: "interfaces"},
								{Name: "interface", Key: map[string]string{"k1": "foo"}},
								{Name: "subinterfaces"},
								{Name: "subinterface", Key: map[string]string{"k1": "foo1", "k2": "bar1"}},
							},
						},
						Update: []*gnmi.Update{
							{
								Path: &gnmi.Path{
									Origin: "",
									Elem: []*gnmi.PathElem{
										{Name: "__juniper_telemetry_header__"},
									},
								},
								Val: &gnmi.TypedValue{
									Value: &gnmi.TypedValue_AnyVal{
										AnyVal: &google_protobuf.Any{
											TypeUrl: "type.googleapis.com/GnmiJuniperTelemetryHeader",
											Value:   hdrInputXpathBytes,
										},
									},
								},
							},
						},
						Delete: []*gnmi.Path{
							{
								Origin: "",
								Elem: []*gnmi.PathElem{
									{Name: "state"},
									{Name: "description"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "rsp-check-not-expecting-eos-juniper-isync-packet-ext",
			err:  false,
			jctx: &JCtx{
				config: Config{
					Host: "127.0.0.1",
					Port: 32767,
					Log: LogConfig{
						Verbose: true,
					},
				},
			},
			rsp: &gnmi.SubscribeResponse{
				Extension: []*gnmi_ext1.Extension{
					{
						Ext: &gnmi_ext1.Extension_RegisteredExt{
							RegisteredExt: &gnmi_ext1.RegisteredExtension{
								Id:  gnmi_ext1.ExtensionID_EID_JUNIPER_TELEMETRY_HEADER,
								Msg: hdrInputExtIsyncBytes,
							},
						},
					},
				},
				Response: &gnmi.SubscribeResponse_Update{
					Update: &gnmi.Notification{
						Timestamp: 1589476296083000000,
						Prefix: &gnmi.Path{
							Origin: "",
							Elem: []*gnmi.PathElem{
								{Name: "interfaces"},
								{Name: "interface", Key: map[string]string{"k1": "foo"}},
								{Name: "subinterfaces"},
								{Name: "subinterface", Key: map[string]string{"k1": "foo1", "k2": "bar1"}},
							},
						},
						Update: []*gnmi.Update{
							{
								Path: &gnmi.Path{
									Origin: "",
									Elem: []*gnmi.PathElem{
										{Name: "state"},
										{Name: "description"},
									},
								},
								Val: &gnmi.TypedValue{
									Value: &gnmi.TypedValue_StringVal{StringVal: "Hello"},
								},
							},
							{
								Path: &gnmi.Path{
									Origin: "",
									Elem: []*gnmi.PathElem{
										{Name: "state"},
										{Name: "mtu"},
									},
								},
								Val: &gnmi.TypedValue{
									Value: &gnmi.TypedValue_IntVal{IntVal: 1500},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "rsp-check-not-expecting-eos-juniper-isync-packet-xpath",
			err:  false,
			jctx: &JCtx{
				config: Config{
					Host: "127.0.0.1",
					Port: 32767,
					Log: LogConfig{
						Verbose: true,
					},
				},
			},
			rsp: &gnmi.SubscribeResponse{
				Response: &gnmi.SubscribeResponse_Update{
					Update: &gnmi.Notification{
						Timestamp: 1589476296083000000,
						Prefix: &gnmi.Path{
							Origin: "",
							Elem: []*gnmi.PathElem{
								{Name: "interfaces"},
								{Name: "interface", Key: map[string]string{"k1": "foo"}},
								{Name: "subinterfaces"},
								{Name: "subinterface", Key: map[string]string{"k1": "foo1", "k2": "bar1"}},
							},
						},
						Update: []*gnmi.Update{
							{
								Path: &gnmi.Path{
									Origin: "",
									Elem: []*gnmi.PathElem{
										{Name: "__juniper_telemetry_header__"},
									},
								},
								Val: &gnmi.TypedValue{
									Value: &gnmi.TypedValue_AnyVal{
										AnyVal: &google_protobuf.Any{
											TypeUrl: "type.googleapis.com/GnmiJuniperTelemetryHeader",
											Value:   hdrInputXpathIsyncBytes,
										},
									},
								},
							},
						},
						Delete: []*gnmi.Path{
							{
								Origin: "",
								Elem: []*gnmi.PathElem{
									{Name: "state"},
									{Name: "description"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "rsp-valid-sync-ipv6",
			err:  false,
			jctx: &JCtx{
				config: Config{
					Host: "[::1]",
					Port: 32767,
					Log: LogConfig{
						Verbose: true,
					},
				},
			},
			rsp: &gnmi.SubscribeResponse{
				Response: &gnmi.SubscribeResponse_SyncResponse{
					SyncResponse: true,
				},
			},
		},
		{
			name: "rsp-valid-updates-ipv6",
			err:  false,
			jctx: &JCtx{
				config: Config{
					Host: "[::1]",
					Port: 32767,
					Log: LogConfig{
						Verbose: true,
					},
				},
			},
			rsp: &gnmi.SubscribeResponse{
				Extension: []*gnmi_ext1.Extension{
					{
						Ext: &gnmi_ext1.Extension_RegisteredExt{
							RegisteredExt: &gnmi_ext1.RegisteredExtension{
								Id:  gnmi_ext1.ExtensionID_EID_JUNIPER_TELEMETRY_HEADER,
								Msg: hdrInputExtBytes,
							},
						},
					},
				},
				Response: &gnmi.SubscribeResponse_Update{
					Update: &gnmi.Notification{
						Timestamp: 1589476296083000000,
						Prefix: &gnmi.Path{
							Origin: "",
							Elem: []*gnmi.PathElem{
								{Name: "interfaces"},
								{Name: "interface", Key: map[string]string{"k1": "foo"}},
								{Name: "subinterfaces"},
								{Name: "subinterface", Key: map[string]string{"k1": "foo1", "k2": "bar1"}},
							},
						},
						Update: []*gnmi.Update{
							{
								Path: &gnmi.Path{
									Origin: "",
									Elem: []*gnmi.PathElem{
										{Name: "state"},
										{Name: "description"},
									},
								},
								Val: &gnmi.TypedValue{
									Value: &gnmi.TypedValue_StringVal{StringVal: "Hello"},
								},
							},
							{
								Path: &gnmi.Path{
									Origin: "",
									Elem: []*gnmi.PathElem{
										{Name: "state"},
										{Name: "mtu"},
									},
								},
								Val: &gnmi.TypedValue{
									Value: &gnmi.TypedValue_IntVal{IntVal: 1500},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "rsp-valid-deletes-ipv6",
			err:  false,
			jctx: &JCtx{
				config: Config{
					Host: "[::1]",
					Port: 32767,
					Log: LogConfig{
						Verbose: true,
					},
				},
			},
			rsp: &gnmi.SubscribeResponse{
				Response: &gnmi.SubscribeResponse_Update{
					Update: &gnmi.Notification{
						Timestamp: 1589476296083000000,
						Prefix: &gnmi.Path{
							Origin: "",
							Elem: []*gnmi.PathElem{
								{Name: "interfaces"},
								{Name: "interface", Key: map[string]string{"k1": "foo"}},
								{Name: "subinterfaces"},
								{Name: "subinterface", Key: map[string]string{"k1": "foo1", "k2": "bar1"}},
							},
						},
						Update: []*gnmi.Update{
							{
								Path: &gnmi.Path{
									Origin: "",
									Elem: []*gnmi.PathElem{
										{Name: "__juniper_telemetry_header__"},
									},
								},
								Val: &gnmi.TypedValue{
									Value: &gnmi.TypedValue_AnyVal{
										AnyVal: &google_protobuf.Any{
											TypeUrl: "type.googleapis.com/GnmiJuniperTelemetryHeader",
											Value:   hdrInputXpathBytes,
										},
									},
								},
							},
						},
						Delete: []*gnmi.Path{
							{
								Origin: "",
								Elem: []*gnmi.PathElem{
									{Name: "state"},
									{Name: "description"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "rsp-check-not-expecting-eos-juniper-isync-packet-ext-ipv6",
			err:  false,
			jctx: &JCtx{
				config: Config{
					Host: "[::1]",
					Port: 32767,
					Log: LogConfig{
						Verbose: true,
					},
				},
			},
			rsp: &gnmi.SubscribeResponse{
				Extension: []*gnmi_ext1.Extension{
					{
						Ext: &gnmi_ext1.Extension_RegisteredExt{
							RegisteredExt: &gnmi_ext1.RegisteredExtension{
								Id:  gnmi_ext1.ExtensionID_EID_JUNIPER_TELEMETRY_HEADER,
								Msg: hdrInputExtIsyncBytes,
							},
						},
					},
				},
				Response: &gnmi.SubscribeResponse_Update{
					Update: &gnmi.Notification{
						Timestamp: 1589476296083000000,
						Prefix: &gnmi.Path{
							Origin: "",
							Elem: []*gnmi.PathElem{
								{Name: "interfaces"},
								{Name: "interface", Key: map[string]string{"k1": "foo"}},
								{Name: "subinterfaces"},
								{Name: "subinterface", Key: map[string]string{"k1": "foo1", "k2": "bar1"}},
							},
						},
						Update: []*gnmi.Update{
							{
								Path: &gnmi.Path{
									Origin: "",
									Elem: []*gnmi.PathElem{
										{Name: "state"},
										{Name: "description"},
									},
								},
								Val: &gnmi.TypedValue{
									Value: &gnmi.TypedValue_StringVal{StringVal: "Hello"},
								},
							},
							{
								Path: &gnmi.Path{
									Origin: "",
									Elem: []*gnmi.PathElem{
										{Name: "state"},
										{Name: "mtu"},
									},
								},
								Val: &gnmi.TypedValue{
									Value: &gnmi.TypedValue_IntVal{IntVal: 1500},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "rsp-check-not-expecting-eos-juniper-isync-packet-xpath-ipv6",
			err:  false,
			jctx: &JCtx{
				config: Config{
					Host: "[::1]",
					Port: 32767,
					Log: LogConfig{
						Verbose: true,
					},
				},
			},
			rsp: &gnmi.SubscribeResponse{
				Response: &gnmi.SubscribeResponse_Update{
					Update: &gnmi.Notification{
						Timestamp: 1589476296083000000,
						Prefix: &gnmi.Path{
							Origin: "",
							Elem: []*gnmi.PathElem{
								{Name: "interfaces"},
								{Name: "interface", Key: map[string]string{"k1": "foo"}},
								{Name: "subinterfaces"},
								{Name: "subinterface", Key: map[string]string{"k1": "foo1", "k2": "bar1"}},
							},
						},
						Update: []*gnmi.Update{
							{
								Path: &gnmi.Path{
									Origin: "",
									Elem: []*gnmi.PathElem{
										{Name: "__juniper_telemetry_header__"},
									},
								},
								Val: &gnmi.TypedValue{
									Value: &gnmi.TypedValue_AnyVal{
										AnyVal: &google_protobuf.Any{
											TypeUrl: "type.googleapis.com/GnmiJuniperTelemetryHeader",
											Value:   hdrInputXpathIsyncBytes,
										},
									},
								},
							},
						},
						Delete: []*gnmi.Path{
							{
								Origin: "",
								Elem: []*gnmi.PathElem{
									{Name: "state"},
									{Name: "description"},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := gnmiHandleResponse(test.jctx, test.rsp)
			if !test.err {
				if err != nil && strings.Contains(err.Error(), gGnmiJtimonIgnoreErrorSubstr) {
					err = nil
				}
				if err != nil {
					var errMsg string
					errMsg = fmt.Sprintf("didn't expect error:%v", err)
					t.Errorf(errMsg)
				}
			}

			if test.err {
				if err == nil {
					var errMsg string
					errMsg = fmt.Sprintf("expected error")
					t.Errorf(errMsg)
				}
			}
		})
	}

	gGnmiUnitTestCoverage = false
	*prom = false
	*noppgoroutines = false
}

// For the below function, better change jtisim?
func TestSubscribegNMI(t *testing.T) {
}

// The below functions should have been already covered by now, so no need to UT them
func TestPublishToPrometheus(t *testing.T) {

}

func TestPublishToInflux(t *testing.T) {

}

func TestGnmiParseHeader(t *testing.T) {

}

func TestGnmiParseNotification(t *testing.T) {

}
