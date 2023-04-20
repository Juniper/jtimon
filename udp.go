package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"regexp"
	"math"
	"strings"
	"log"
	"os"
	"io"
	"os/signal"
	"net"
	"time"
	"sync"
	"syscall"

	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/reflect/protopath"
	"google.golang.org/protobuf/reflect/protorange"
	"google.golang.org/protobuf/reflect/protoreflect"
	"github.com/golang/protobuf/proto"
	"github.com/jhump/protoreflect/desc/protoparse"
	"github.com/influxdata/influxdb/client/v2"

	gogoproto "github.com/gogo/protobuf/proto"
	gogodescriptor "github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	protoschema "github.com/Juniper/jtimon/schema"
	tt "github.com/Juniper/jtimon/schema/telemetry_top"
)

func parseProtos(jctx *JCtx) string {
	var extNames string
	if len(jctx.config.UDP.Protos) != 0 {
		parser := protoparse.Parser{ImportPaths: []string{"./schema"}, IncludeSourceCodeInfo: true}
		protos, err := parser.ParseFiles(jctx.config.UDP.Protos...)
		if err != nil {
			jLog(jctx, fmt.Sprintf("parse proto files error: %v\n", err))
		}
		for _, fileDescriptors := range protos {
			for _, extDescriptor := range fileDescriptors.GetExtensions() {
				extNames += extDescriptor.GetName() + "\n"
			}
		}
	}
	return extNames
}

func processUDPData(jctx *JCtx, ts *protoschema.TelemetryStream) map[string]map[string]interface{} {
	mData := make(map[string]map[string]interface{})
	var mName string
	b, err := json.MarshalIndent(ts, "", "  ")
	if err != nil {
		jLog(jctx, fmt.Sprintf("processUDPData: json marshalindent error: %v\n", err))
	}

	data := make(map[string]interface{})
	if err = json.Unmarshal(b, &data)
	 err != nil {
		jLog(jctx, fmt.Sprintf("processUDPData: json unmarshal error: %v\n", err))
	}
	delete(data, "enterprise")
	property:= make(map[string]interface{})
	keys := make(map[string]interface{})
	extNames := parseProtos(jctx)

	if gogoproto.HasExtension(ts.Enterprise, protoschema.E_JuniperNetworks) {
		jns_i, err := gogoproto.GetExtension(ts.Enterprise, protoschema.E_JuniperNetworks)
		if err != nil {
			jLog(jctx, fmt.Sprintf("processUDPData: cant get extension: %v\n", err))
		}
		switch jns := jns_i.(type) {
		case *protoschema.JuniperNetworksSensors:
			for _, extDescriptor := range gogoproto.RegisteredExtensions(jns) {
				if gogoproto.HasExtension(jns, extDescriptor) {
					if len(jctx.config.UDP.Protos) != 0 && !strings.Contains(extNames, extDescriptor.Name) {
						continue
					}
					extObj, err := gogoproto.GetExtension(jns, extDescriptor)
					if err != nil {
						jLog(jctx, fmt.Sprintf("processUDPData: cant get extension: %v\n", err))
					}

					extMsg := extObj.(gogodescriptor.Message)
					protoMsg := extMsg.(proto.Message)
					mr := proto.MessageReflect(protoMsg)

					var xpath string
					var xpath0 string
					var xpath_tmp string
					var root string
					var list_idx int
					var level_idx int
					var key0 string
					var tag_idx int
					list_idx0 := -1
					protorange.Options{
						Stable: true,
					}.Range(mr,
						func(p protopath.Values) error {
							var fd protoreflect.FieldDescriptor
							last := p.Index(-1)
							beforeLast := p.Index(-2)

							var v2 string
							switch v := last.Value.Interface().(type) {
							case string, []byte:
								v2 = fmt.Sprintf("%s", v)
							case float64:
								temp := math.Pow(v, float64(2))
								val := math.Round(v * temp) / temp
								v2 = fmt.Sprintf("%v", val)
							case uint64:
								v2 = fmt.Sprintf("%v", int(v))
							default:
								v2 = fmt.Sprintf("%v", v)
							}

							var key string
							var tags []string
							var keysOnly bool
							switch last.Step.Kind() {
							case protopath.ListIndexStep:
								key = ""
								keysOnly = true
								if list_idx != list_idx0 {
									xpath0 = xpath_tmp + key
									xpath = xpath0
								}

								switch mr := beforeLast.Value.Interface().(type) {
								case protoreflect.List:
									mTags := make(map[string]interface{})
									for i := 0; i < mr.Len(); i++ {
										var leafKey string
										msg0 := mr.Get(i)
										switch msg0.Interface().(type) {
											case protoreflect.Message:
												fd = beforeLast.Step.FieldDescriptor()
												m0 := msg0.Message()
												m0.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
													v2 = fmt.Sprintf("%v", v)
													fieldName := string(fd.Name())
													opt2 := fd.Options().(*descriptorpb.FieldOptions)
													keysMap := make(map[string]string)
													if proto.HasExtension(opt2, tt.E_TelemetryOptions) {
														opt, err := proto.GetExtension(opt2, tt.E_TelemetryOptions)
														if err != nil {
															log.Fatalln("cant get extension: ", err)
														}
														IsKey := opt.(*tt.TelemetryFieldOptions).GetIsKey()
														if IsKey == true {
															keysMap[fieldName] = v2
														} else {
															keysOnly = false
														}
													} else {
														keysOnly = false
													}
													for fieldName, val := range keysMap {
														keys[fieldName] = val
														leafKey = leafKey + " and "+ fieldName +"='" + val + "'"
													}
													return true
												})
												if keysOnly == false {
													if len(leafKey) >= 5 {
													leafKey = leafKey[5:]
													leafKey = "[" + leafKey + "]"
													}
												}
											default:
										}
										tags = append(tags, leafKey)

									}
									if len(tags) > 0 && tag_idx < len(tags) {
										key0 = key
										key = tags[tag_idx]

										if system_id, ok := data["system_id"]; ok {
											mTags["system_id"] = fmt.Sprintf("%v", system_id)
										}
										if sensor_name, ok := data["sensor_name"]; ok {
											mTags["sensor_name"] = fmt.Sprintf("%v", sensor_name)
										}
										if keysOnly == false {
											key0 = key
										} else {
											key = key0
										}
										tag_idx += 1
									}
								}
								list_idx = last.Step.ListIndex()
								if list_idx != list_idx0 {
									xpath0 = xpath_tmp + key
									xpath = xpath0
								}
								if list_idx == 0 && keysOnly == false {
									xpath0 = xpath0 + key
								}
								if list_idx != 0 {
									xpath_tmp = root
								}
							case protopath.FieldAccessStep:
								fd = last.Step.FieldDescriptor()
								props := make(map[string]interface{})
								switch last.Value.Interface().(type) {
								case protoreflect.List:
									if list_idx > list_idx0 {
										xpath0 = "/"+ fd.TextName()
										root = xpath0
										xpath_tmp = xpath0
										level_idx = 0
									} else {
										xpath = xpath0 + "/"+fd.TextName()
									}
									level_idx = level_idx + 1
								case protoreflect.Message:
									xpath0 = xpath_tmp + key0 + "/"+ fd.TextName()
									xpath = xpath0
								default:
									opt2 := fd.Options().(*descriptorpb.FieldOptions)
									if proto.HasExtension(opt2, tt.E_TelemetryOptions) {
										opt, err := proto.GetExtension(opt2, tt.E_TelemetryOptions)
										if err != nil {
											log.Fatalln("Cant get extension: ", err)
										}
										IsKey := opt.(*tt.TelemetryFieldOptions).GetIsKey()
										if IsKey == true {
											if list_idx == 0 && level_idx <= 1 {
												xpath_tmp = xpath_tmp + key
											}
										} else {
											xpath = xpath0 + "/"+fd.TextName()
											props[xpath] = v2
										}
									} else {
										xpath = xpath0 + "/"+ fd.TextName()
										props[xpath] = v2
										xpath = xpath0
									}
								}
								if _, ok := property[strconv.Itoa(tag_idx)]; !ok {
									property[strconv.Itoa(tag_idx)] = props
								} else {
									if m, ok := property[strconv.Itoa(tag_idx)].(map[string]interface{}); ok {
										for k, v := range props {
											m[k] = v
										}
									}
								}
								mName = root
								list_idx0 = list_idx
							}
							return nil
						},
						func(p protopath.Values) error {
							return nil
						},
					)
				}
			}
		}
	}
	delete(property, "0")
	data["Properties"] = property
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		jLog(jctx, fmt.Sprintf("processUDPData: JSON indent error: %v\n", err))
	}
	jLog(jctx, fmt.Sprintln(("\n******** Data which will be written to DB is: ********")))
	jLog(jctx, fmt.Sprintf("%s\n", bytes))
	mData[mName] = data
	return mData
}

func (jctx *JCtx) udpSignalHandler() {
	sigchan := make(chan os.Signal, 10)
	signal.Notify(sigchan, os.Interrupt, syscall.SIGHUP)
	for {
		select {
		case s := <-sigchan:
			switch s {
			case syscall.SIGHUP:
				restart := false
				err := ConfigRead(jctx, false, &restart)
				if err != nil {
					log.Println(err)
				} else if restart {
					jctx.control <- syscall.SIGHUP
					jLog(jctx, fmt.Sprintf("UDP config re-parse, data streaming has not started yet"))
				}
			case os.Interrupt:
				jctx.control <- os.Interrupt
				jLog(jctx, fmt.Sprintf("UDP dialing has been interrupted(SIGINT)"))
				return
			}
		}
	}
}

func udpSubscribeJunos(conn net.PacketConn, jctx *JCtx) SubErrorCode {
    go func() {
        jLog(jctx, fmt.Sprintf("Receiving UDP telemetry data from %s:%d\n", jctx.config.UDP.Host, jctx.config.UDP.Port))
        buffer := make([]byte, 102400)

        for {
            n, _, err := conn.ReadFrom(buffer)
            if err == io.EOF {
                printSummary(jctx)
                return
            }
            if err != nil {
                jLog(jctx, fmt.Sprintf("Read UDP packets failed, %v\n", err))
                return
            }
            if n == 0 {
                jLog(jctx, fmt.Sprintf("%v No UDP packet received\n", conn))
                return
            }

            ts := &protoschema.TelemetryStream{}
            if err := proto.Unmarshal(buffer[:n], ts); err != nil {
                jLog(jctx, fmt.Sprintf("Failed to parse TelemetryStream: %v\n", err))
            }

			rtime := time.Now()
            if *noppgoroutines {
				mData := processUDPData(jctx, ts)
                udpAddIDB(mData, jctx, rtime)
            } else {
                go func(ts *protoschema.TelemetryStream) {
					mData := processUDPData(jctx, ts)
                    udpAddIDB(mData, jctx, rtime)
                }(ts)
            }
        }
    }()

    for {
        select {
        case s := <-jctx.control:
            switch s {
            case syscall.SIGHUP:
                return SubRcSighupRestart
            case os.Interrupt:
                return SubRcSighupNoRestart
            }
        }
    }
}

// Takes in XML path with predicates and returns map of tags+values
func extractTags(jctx *JCtx, xmlpath string) map[string]string {
	// Example :
	// 		xmlpath /interface_info[if_name='ge-0/0/0' and snmp_if_index='0']
	//		tags  /interface_info/@if_name:ge-0/0/0
	//            /interface_info/@snmp_if_index:0
	jctx.influxCtx.reKey = regexp.MustCompile(UDPMatchExpressionKey)
	_, tags := spitTagsNPath(jctx, xmlpath)
	// Add tags without key-value pairs
	for _, tag := range strings.Split(xmlpath, "/") {
		if !strings.HasPrefix(tag, "@") {
			tags[strings.Replace(tag, "-", "_", -1)] = ""
		}
	}
	// Remove keys without values
	for tagKey, tagValue := range tags {
		if tagValue == "" {
			delete(tags, tagKey)
		}
	}
	return tags
}

func udpAddIDB(mData map[string]map[string]interface{}, jctx *JCtx, rtime time.Time) {
	cfg := jctx.config

	bp, err := client.NewBatchPoints(client.BatchPointsConfig{
		Database:        cfg.Influx.Dbname,
		Precision:       "ns",
		RetentionPolicy: cfg.Influx.RetentionPolicy,
	})

	if err != nil {
		jLog(jctx, fmt.Sprintf("NewBatchPoints failed, error: %v\n", err))
		return
	}

	for measurement, data := range mData {
		var mName string
		if jctx.config.Influx.Measurement == "" {
			mName = measurement
		} else {
			mName = jctx.config.Influx.Measurement
		}
		fields := make(map[string]interface{})
		tags := make(map[string]string)
		for dataKey, val := range data {
			if dataKey != "Properties" {
				if dataKey == "system_id" || dataKey == "sensor_name" {
					tags[dataKey] = fmt.Sprintf("%v", val)
				} else {
					fields[dataKey] = val
				}
			}
		}
		for dataKey, _ := range data {
			if dataKey == "Properties" {
				for _, counters := range data["Properties"].(map[string]interface{}) {
					var cnt int
					xpathTags := make(map[string]string)
					for k, val := range counters.(map[string]interface{}) {
						cnt += 1
						if cnt == 1 {
							xpathTags = extractTags(jctx, k)
							xpathTags["system_id"] = tags["system_id"]
							xpathTags["sensor_name"] = tags["sensor_name"]
						}
						splitInput := strings.Split(k, "/")
						lastElement := splitInput[len(splitInput)-1]
						tags = xpathTags
						intVal, err := strconv.ParseInt(fmt.Sprintf("%s", val), 10, 64)
						if err != nil {
							fields[lastElement] = val
						} else {
							fields[lastElement] = intVal
						}

					}
					pt, err := client.NewPoint(mName, tags, fields, rtime)
					if err != nil {
						jLog(jctx, fmt.Sprintf("Could not get NewPoint (first point): %v\n", err))
						continue
					}
					if IsVerboseLogging(jctx) {
						jLog(jctx, fmt.Sprintf("Tags: %+v\n", pt.Tags()))
						if f, err := pt.Fields(); err == nil {
							jLog(jctx, fmt.Sprintf("KVs : %+v\n", f))
						}
					}
					bp.AddPoint(pt)
				}
			}
		}
	}

	if err := (*jctx.influxCtx.influxClient).Write(bp); err != nil {
		jLog(jctx, fmt.Sprintf("Batch DB write failed: %v\n", err))
	} else {
		jLog(jctx, fmt.Sprintf("Updated %d records in database %s", len(bp.Points()), cfg.Influx.Dbname))
	}
}

func udpInit(jctx *JCtx) error {
udpdial:
		err := ConfigRead(jctx, true, nil)
		if err != nil {
			log.Println(err)
			return err
		}

		udpHostname := jctx.config.UDP.Host + ":" + strconv.Itoa(jctx.config.UDP.Port)
		go jctx.udpSignalHandler()

		jLog(jctx, fmt.Sprintf("Dialing UDP host %s", udpHostname))
		ln, err := net.ListenPacket("udp", udpHostname)
		if err != nil {
			jLog(jctx, fmt.Sprintf("[%s] could not listen on: %v", jctx.config.UDP.Host, err))
			return err
		}

		fmt.Println("Calling udpSubscribe() :::", jctx.file)
		udpCode := udpSubscribeJunos(ln, jctx)
		fmt.Println("Returns udpSubscribe() :::", jctx.file, "UDPCODE ::: ", udpCode)

		switch udpCode {
		case SubRcSighupRestart:
			jLog(jctx, fmt.Sprintf("sighup detected, re-dial with new config for UDP host %s", udpHostname))
			goto udpdial
		case SubRcSighupNoRestart:
			jLog(jctx, fmt.Sprintf("Streaming will be stopped (SIGINT) for UDP host %s", udpHostname))
			return fmt.Errorf("UDP dialing for %s has been interrupted", udpHostname)
		}
		ln.Close()

	return nil
}

func udpWork(configFiles *[]string) {
	var wg sync.WaitGroup
	jctx := JCtx{
		wg:        &wg,
	}
	for _, fileName := range *configFiles {
		jctx.file = fileName
		jctx.stats = statsCtx{
			startTime: time.Now(),
		}
		if err := udpInit(&jctx); err == nil {
			jctx.wg.Add(1)
		}
	}
	jctx.wg.Wait()
}
