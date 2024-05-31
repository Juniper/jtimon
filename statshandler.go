package main

import (
	"fmt"
	gnmi_juniper_header_ext "github.com/Juniper/jtimon/gnmi/gnmi_juniper_header_ext"
	"log"
	"os"
	"sync"
	"time"

	gnmi_pb "github.com/Juniper/jtimon/gnmi/gnmi"
	na_pb "github.com/Juniper/jtimon/telemetry"
	proto "github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc/stats"
)

type statsCtx struct {
	sync.Mutex               // guarding following stats
	startTime                time.Time
	totalIn                  uint64
	totalKV                  uint64
	totalInPayloadLength     uint64
	totalInPayloadWireLength uint64
	totalInHeaderWireLength  uint64
}

type kpiStats struct {
	SensorName                   string
	Path                         string
	Streamed_path                string
	Component                    string
	SequenceNumber               uint64
	ComponentId                  uint32
	SubComponentId               uint32
	Timestamp                    uint64
	notif_timestamp              int64
	re_stream_creation_timestamp uint64
	re_payload_get_timestamp     uint64
}

type statshandler struct {
	jctx *JCtx
}

func (h *statshandler) TagConn(ctx context.Context, info *stats.ConnTagInfo) context.Context {
	return ctx
}

func (h *statshandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	return ctx
}

func (h *statshandler) HandleConn(ctx context.Context, s stats.ConnStats) {
	switch s.(type) {
	case *stats.ConnBegin:
	case *stats.ConnEnd:
	default:
	}
}

func (h *statshandler) HandleRPC(ctx context.Context, s stats.RPCStats) {
	h.jctx.stats.Lock()
	defer h.jctx.stats.Unlock()

	switch s.(type) {
	case *stats.InHeader:
		h.jctx.stats.totalInHeaderWireLength += uint64(s.(*stats.InHeader).WireLength)
	case *stats.OutHeader:
	case *stats.OutPayload:
	case *stats.InPayload:
		h.jctx.stats.totalInPayloadLength += uint64(s.(*stats.InPayload).Length)
		h.jctx.stats.totalInPayloadWireLength += uint64(s.(*stats.InPayload).WireLength)
		if *stateHandler && h.jctx.config.InternalJtimon.CsvStats {
			switch v := (s.(*stats.InPayload).Payload).(type) {
			case *na_pb.OpenConfigData:
				updateStats(h.jctx, v, false)
				for idx, kv := range v.Kv {
					updateStatsKV(h.jctx, false, 0)
					switch kvvalue := kv.Value.(type) {
					case *na_pb.KeyValue_UintValue:
						if kv.Key == "__timestamp__" {
							var re_c_ts uint64 = 0
							var re_p_get_ts uint64 = 0
							if len(v.Kv) > idx+2 {
								nextKV := v.Kv[idx+1]
								if nextKV.Key == "__junos_re_stream_creation_timestamp__" {
									re_c_ts = nextKV.GetUintValue()
								}
								nextnextKV := v.Kv[idx+2]
								if nextnextKV.Key == "__junos_re_payload_get_timestamp__" {
									re_p_get_ts = nextnextKV.GetUintValue()
								}
							}

							//"sensor-path", "sequence-number", "component-id", "sub-component-id", "packet-size", "p-ts", "e-ts", "re-stream-creation-ts", "re-payload-get-ts"))
							h.jctx.config.InternalJtimon.csvLogger.Printf(
								fmt.Sprintf("%s,%d,%d,%d,%d,%d,%d,%d,%d\n",
									v.Path, v.SequenceNumber, v.ComponentId, v.SubComponentId, s.(*stats.InPayload).Length, v.Timestamp, kvvalue.UintValue, re_c_ts, re_p_get_ts))
						}
					}
				}
			case *gnmi_pb.SubscribeResponse:
				stat := getKPIStats(v)
				if stat != nil && stat.Timestamp != 0 {
					path := stat.SensorName + ":" + stat.Streamed_path + ":" + stat.Path + ":" + stat.Component
					h.jctx.config.InternalJtimon.csvLogger.Printf(
						fmt.Sprintf("%s,%d,%d,%d,%d,%d,%d,%d,%d\n",
							path, stat.SequenceNumber, stat.ComponentId, stat.SubComponentId,
							s.(*stats.InPayload).Length, stat.notif_timestamp, int64(stat.Timestamp*uint64(1000000)),
							int64(stat.re_stream_creation_timestamp*uint64(1000000)),
							int64(stat.re_payload_get_timestamp*uint64(1000000)),
						),
					)
				}
			}
		}
	case *stats.InTrailer:
	case *stats.End:
	default:
	}
}

func getKPIStats(subResponse *gnmi_pb.SubscribeResponse) *kpiStats {

	stats := new(kpiStats)
	notfn := subResponse.GetUpdate()
	if notfn == nil {
		return nil
	}
	stats.notif_timestamp = notfn.Timestamp
	extns := subResponse.GetExtension()

	if extns != nil {
		extn := extns[0]
		if extn != nil {
			var hdr gnmi_juniper_header_ext.GnmiJuniperTelemetryHeaderExtension

			reg_extn := extn.GetRegisteredExt()
			msg := reg_extn.GetMsg()
			err := proto.Unmarshal(msg, &hdr)
			if err != nil {
				log.Fatal("unmarshaling error: ", err)
			}

			stats.ComponentId = hdr.ComponentId
			stats.SequenceNumber = hdr.SequenceNumber
			stats.Path = hdr.SubscribedPath
			stats.SubComponentId = hdr.SubComponentId
			stats.Component = hdr.Component
			stats.Streamed_path = hdr.StreamedPath
			stats.SensorName = hdr.SensorName

			if hdr.ExportTimestamp > 0 {
				stats.Timestamp = uint64(hdr.ExportTimestamp)
			}
			if hdr.PayloadGetTimestamp > 0 {
				stats.re_payload_get_timestamp = uint64(hdr.PayloadGetTimestamp)
			}
			if hdr.StreamCreationTimestamp > 0 {
				stats.re_stream_creation_timestamp = uint64(hdr.StreamCreationTimestamp)
			}
		}
	}
	return stats

}

func updateStats(jctx *JCtx, ocData *na_pb.OpenConfigData, needLock bool) {
	if !*stateHandler {
		return
	}
	if needLock {
		jctx.stats.Lock()
		defer jctx.stats.Unlock()
	}
	jctx.stats.totalIn++
}

func updateStatsKV(jctx *JCtx, needLock bool, count uint64) {
	if !*stateHandler {
		return
	}

	if needLock {
		jctx.stats.Lock()
		defer jctx.stats.Unlock()
	}
	jctx.stats.totalKV = jctx.stats.totalKV + count
}

func periodicStats(jctx *JCtx) {
	if !*stateHandler {
		return
	}
	pstats := jctx.config.Log.PeriodicStats
	if pstats == 0 {
		return
	}

	headerCounter := 0
	for {
		tickChan := time.NewTicker(time.Second * time.Duration(pstats)).C
		<-tickChan

		// Do nothing if we haven't heard back anything from the device

		jctx.stats.Lock()
		if jctx.stats.totalIn == 0 {
			jctx.stats.Unlock()
			continue
		}

		s := fmt.Sprintf("\n")

		// print header
		if headerCounter%100 == 0 {
			s += "+------------------------------+--------------------+--------------------+--------------------+--------------------+\n"
			s += "|         Timestamp            |        KV          |      Packets       |       Bytes        |     Bytes(wire)    |\n"
			s += "+------------------------------+--------------------+--------------------+--------------------+--------------------+\n"
		}

		s += fmt.Sprintf("| %s | %18v | %18v | %18v | %18v |\n", time.Now().Format(time.UnixDate),
			jctx.stats.totalKV,
			jctx.stats.totalIn,
			jctx.stats.totalInPayloadLength,
			jctx.stats.totalInPayloadWireLength)
		jctx.stats.Unlock()
		headerCounter++
		if s != "" {
			jLog(jctx, fmt.Sprintf("%s\n", s))
		}
	}
}

func printSummary(jctx *JCtx) {
	if !*stateHandler {
		return
	}

	endTime := time.Since(jctx.stats.startTime)

	s := fmt.Sprintf("\nCollector Stats for %s:%d (Run time : %s)\n", jctx.config.Host, jctx.config.Port, endTime)
	s += fmt.Sprintf("%-12v : in-packets\n", jctx.stats.totalIn)
	s += fmt.Sprintf("%-12v : data points (KV pairs)\n", jctx.stats.totalKV)

	s += fmt.Sprintf("%-12v : in-header wirelength (bytes)\n", jctx.stats.totalInHeaderWireLength)
	s += fmt.Sprintf("%-12v : in-payload length (bytes)\n", jctx.stats.totalInPayloadLength)
	s += fmt.Sprintf("%-12v : in-payload wirelength (bytes)\n", jctx.stats.totalInPayloadWireLength)
	if uint64(endTime.Seconds()) != 0 {
		s += fmt.Sprintf("%-12v : throughput (bytes per seconds)\n", jctx.stats.totalInPayloadLength/uint64(endTime.Seconds()))
	}

	s += fmt.Sprintf("\n")
	jLog(jctx, fmt.Sprintf("\n%s\n", s))
}

func isCsvStatsEnabled(jctx *JCtx) bool {
	if *stateHandler && jctx.config.InternalJtimon.CsvStats {
		return true
	}
	return false
}

func csvStatsLogInit(jctx *JCtx) {
	if !*stateHandler && !jctx.config.InternalJtimon.CsvStats {
		return
	}
	var out *os.File
	var err error

	csvStatsFile := "csv-stats.csv"
	if jctx.config.InternalJtimon.CsvLog == "" {
		jctx.config.InternalJtimon.CsvLog = csvStatsFile
	}

	out, err = os.OpenFile(jctx.config.InternalJtimon.CsvLog, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		log.Printf("Could not create csv stats file(%s): %v\n", csvStatsFile, err)
	}

	if out != nil {
		flags := 0

		jctx.config.InternalJtimon.csvLogger = log.New(out, "", flags)
		jctx.config.InternalJtimon.csvOut = out

		log.Printf("Writing stats in %s for %s:%d [in csv format]\n",
			jctx.config.InternalJtimon.CsvLog, jctx.config.Host, jctx.config.Port)
	}
}

func csvStatsLogStop(jctx *JCtx) {
	if jctx.config.InternalJtimon.csvOut != nil {
		jctx.config.InternalJtimon.csvOut.Close()
		jctx.config.InternalJtimon.csvOut = nil
		jctx.config.InternalJtimon.csvLogger = nil
	}
}
