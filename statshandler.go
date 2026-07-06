package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gnmi_ext1 "github.com/Juniper/jtimon/gnmi/gnmi_ext"
	gnmi_juniper_header_ext "github.com/Juniper/jtimon/gnmi/gnmi_juniper_header_ext"

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
	Eom                          bool
}

type xpathStats struct {
	sequence_number          uint64
	total_bytes              uint64
	total_packets            uint64
	max_pkt_size             uint64
	min_pkt_size             uint64
	avg_pkt_size             uint64
	max_latency              uint64
	min_latency              uint64
	avg_latency              uint64
	cur_inter_pkt_delay      uint64
	wrap_inter_pkt_delay     string
	cur_wrap_inter_pkt_delay string
	size_pkts_wrap           []float64
	latency_wrap             []float64
	delay_pkts_wrap          []string
	percentile_pkt_size      string
	percentile_latency       string
	max_inter_pkt_delay      uint64
	min_inter_pkt_delay      uint64
	avg_inter_pkt_delay      uint64
	prev_timestamp           uint64
	packets_per_wrap         uint64
	cur_packets_per_wrap     uint64
	bytes_per_wrap           uint64
	cur_bytes_per_wrap       uint64
	wrap_time                uint64
	wrap_start_timestamp     uint64
	initial_drop_counter     uint64
	periodic_drop_counter    uint64
	wrap_counter             uint64
	Eos                      bool
}

var periodic_stats_updated bool = false

// var xpath_initialsync_stats = make(map[string]xpathStats)
// var initialsync_stats_updated bool = false

// statsChanBufSize bounds the async stats queue. With the non-blocking,
// drop-on-full enqueue in HandleRPC this caps the memory held by in-flight
// stats samples while absorbing short bursts at high packet rates.
const statsChanBufSize = 16384

// statPkt carries the minimal data captured on the gRPC receive goroutine so
// that all heavy stats processing can run on a dedicated worker goroutine.
// payload is the already-decoded proto message (gRPC allocates a fresh message
// per Recv, so retaining this reference after HandleRPC returns is safe).
type statPkt struct {
	payload    interface{}
	wireLength int
	length     int
}

type statshandler struct {
	jctx *JCtx

	// statsCh decouples per-packet stats processing from the gRPC receive
	// goroutine. HandleRPC does a non-blocking send and a dedicated worker
	// drains it. This keeps stats work off the receive path so it can never
	// stall draining of the stream (which would back-pressure the device via
	// HTTP/2 flow control and slow the router's telemetry writes).
	statsCh  chan *statPkt
	done     chan struct{}
	stopOnce sync.Once
	dropped  uint64

	// The maps and rate-sampling cursor below are owned exclusively by the
	// worker goroutine (single writer), so they need no locking. Keeping them
	// per-connection (instead of package-global) also fixes the previous
	// cross-connection data race and stats key collisions when monitoring
	// multiple devices in one process.
	xpathStats         map[string]xpathStats
	previousXpathStats map[string]xpathStats
	previousSecs       uint64
}

// newStatsHandler creates a stats handler and starts its background worker.
func newStatsHandler(jctx *JCtx) *statshandler {
	h := &statshandler{
		jctx:               jctx,
		statsCh:            make(chan *statPkt, statsChanBufSize),
		done:               make(chan struct{}),
		xpathStats:         make(map[string]xpathStats),
		previousXpathStats: make(map[string]xpathStats),
	}
	go h.statsWorker()
	return h
}

// statsWorker drains statsCh and performs all the heavy per-packet stats
// processing off the gRPC receive path. It exits when the connection ends.
func (h *statshandler) statsWorker() {
	for {
		select {
		case <-h.done:
			return
		case pkt := <-h.statsCh:
			h.processStatsPkt(pkt)
			h.printStatsRate()
		}
	}
}

// stop signals the worker goroutine to exit. Safe to call multiple times.
func (h *statshandler) stop() {
	h.stopOnce.Do(func() { close(h.done) })
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
		h.stop()
	default:
	}
}

// percentile computes the given percentile of data. The caller MUST sort data
// (ascending) before invoking this, so repeated calls over the same slice do
// not re-sort on every invocation.
func percentile(data []float64, percentile float64) float64 {
	index := (percentile / 100) * float64(len(data))

	// Handle edge cases
	if len(data) == 0 {
		return 0
	}
	if index < 1 {
		return data[0]
	} else if index >= float64(len(data)) {
		return data[len(data)-1]
	}

	// Interpolate between values
	lower := int(math.Floor(index))
	upper := int(math.Ceil(index))
	weight := index - float64(lower)
	return data[lower-1]*(1-weight) + data[upper-1]*weight
}

func (h *statshandler) HandleRPC(ctx context.Context, s stats.RPCStats) {

	switch s.(type) {
	case *stats.InHeader:
		h.jctx.stats.Lock()
		h.jctx.stats.totalInHeaderWireLength += uint64(s.(*stats.InHeader).WireLength)
		h.jctx.stats.Unlock()
	case *stats.OutHeader:
	case *stats.OutPayload:
	case *stats.InPayload:
		in := s.(*stats.InPayload)
		h.jctx.stats.Lock()
		h.jctx.stats.totalInPayloadLength += uint64(in.Length)
		h.jctx.stats.totalInPayloadWireLength += uint64(in.WireLength)
		h.jctx.stats.Unlock()
		if *stateHandler {
			// Hand the payload to the async worker (non-blocking). If the worker
			// is behind, drop the stats sample rather than block the gRPC receive
			// goroutine, which would back-pressure the device via HTTP/2 flow
			// control and slow the router's telemetry writes.
			pkt := &statPkt{payload: in.Payload, wireLength: in.WireLength, length: in.Length}
			select {
			case h.statsCh <- pkt:
			default:
				atomic.AddUint64(&h.dropped, 1)
			}
		}
	case *stats.InTrailer:
	case *stats.End:
	default:
	}
}

// processStatsPkt performs the heavy per-packet stats accounting on the worker
// goroutine, off the gRPC receive path.
func (h *statshandler) processStatsPkt(pkt *statPkt) {
	switch v := pkt.payload.(type) {
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
					if h.jctx.config.InternalJtimon.CsvLog != "" {
						//"sensor-path", "sequence-number", "component-id", "sub-component-id", "packet-size", "p-ts", "e-ts", "re-stream-creation-ts", "re-payload-get-ts"))
						h.jctx.config.InternalJtimon.csvLogger.Printf(
							fmt.Sprintf("%s,%d,%d,%d,%d,%d,%d,%d,%d\n",
								v.Path, v.SequenceNumber, v.ComponentId, v.SubComponentId, pkt.length, v.Timestamp, kvvalue.UintValue, re_c_ts, re_p_get_ts))
						if _, exists := h.xpathStats[v.Path]; !exists {
							h.xpathStats[v.Path] = xpathStats{total_bytes: 0, total_packets: 0, max_pkt_size: 0, min_pkt_size: 0, avg_pkt_size: 0, max_latency: 0, min_latency: 0, avg_latency: 0, max_inter_pkt_delay: 0, min_inter_pkt_delay: 0, avg_inter_pkt_delay: 0, prev_timestamp: 0}
						}
					}
					xstats := h.xpathStats[v.Path]
					xstats.total_bytes += uint64(pkt.wireLength)
					xstats.total_packets++
					if xstats.max_pkt_size < uint64(pkt.wireLength) {
						xstats.max_pkt_size = uint64(pkt.wireLength)
					}
					if xstats.min_pkt_size == 0 || xstats.min_pkt_size > uint64(pkt.wireLength) {
						xstats.min_pkt_size = uint64(pkt.wireLength)
					}
					xstats.avg_pkt_size = xstats.total_bytes / xstats.total_packets

					h.xpathStats[v.Path] = xstats
				}
			}
		}
	case *gnmi_pb.SubscribeResponse:
		stat := h.getKPIStats(v)
		if stat != nil && stat.Timestamp != 0 {
			path := stat.SensorName + ":" + strings.TrimSuffix(stat.Streamed_path, "/") + ":" + strings.TrimSuffix(stat.Path, "/") + ":" + stat.Component + ":" + fmt.Sprintf("%d", stat.ComponentId) + ":" + fmt.Sprintf("%d", stat.SubComponentId)
			if h.jctx.config.InternalJtimon.CsvLog != "" {
				h.jctx.config.InternalJtimon.csvLogger.Printf(
					fmt.Sprintf("%s,%d,%d,%d,%d,%d,%d,%d,%d\n",
						path, stat.SequenceNumber, stat.ComponentId, stat.SubComponentId,
						pkt.length, stat.notif_timestamp, int64(stat.Timestamp*uint64(1000000)),
						int64(stat.re_stream_creation_timestamp*uint64(1000000)),
						int64(stat.re_payload_get_timestamp*uint64(1000000)),
					),
				)
			}
			if _, exists := h.xpathStats[path]; !exists {
				h.xpathStats[path] = xpathStats{
					total_bytes:              0,
					total_packets:            0,
					max_pkt_size:             0,
					min_pkt_size:             0,
					avg_pkt_size:             0,
					max_latency:              0,
					min_latency:              0,
					avg_latency:              0,
					cur_inter_pkt_delay:      0,
					wrap_inter_pkt_delay:     "",
					cur_wrap_inter_pkt_delay: "",
					percentile_pkt_size:      "",
					size_pkts_wrap:           []float64{},
					delay_pkts_wrap:          []string{},
					latency_wrap:             []float64{},
					percentile_latency:       "",
					max_inter_pkt_delay:      0,
					min_inter_pkt_delay:      0,
					avg_inter_pkt_delay:      0,
					prev_timestamp:           0,
					packets_per_wrap:         0,
					cur_packets_per_wrap:     0,
					bytes_per_wrap:           0,
					cur_bytes_per_wrap:       0,
					wrap_time:                0,
					wrap_start_timestamp:     0,
					initial_drop_counter:     0,
					periodic_drop_counter:    0,
					wrap_counter:             0,
					Eos:                      false,
				}
			}

			xstats := h.xpathStats[path]
			xstats.Eos = h.jctx.receivedSyncRsp

			if stat.SequenceNumber >= uint64(1<<21) && stat.SequenceNumber <= (uint64(1<<22)-1) {
				// Initial sync mdoe
				if xstats.sequence_number != 0 && stat.SequenceNumber != xstats.sequence_number+1 {
					xstats.initial_drop_counter++
				}
			} else if stat.SequenceNumber >= uint64(1<<20) && stat.SequenceNumber <= (uint64(1<<21)-1) {
				// ONCE mode
				if xstats.sequence_number != 0 && stat.SequenceNumber != xstats.sequence_number+1 {
					xstats.initial_drop_counter++
				}
			} else if stat.SequenceNumber >= 0 && stat.SequenceNumber <= (uint64(1<<20)-1) {
				// Periodic mode
				if xstats.sequence_number != 0 && stat.SequenceNumber != xstats.sequence_number+1 {
					if xstats.sequence_number <= (uint64(1<<20) - 1) {
						xstats.periodic_drop_counter++
					}
				}
			}

			xstats.sequence_number = stat.SequenceNumber
			xstats.cur_packets_per_wrap += 1
			xstats.cur_bytes_per_wrap += uint64(pkt.wireLength)

			xstats.total_bytes += uint64(pkt.wireLength)
			xstats.total_packets++
			if xstats.max_pkt_size < uint64(pkt.wireLength) {
				xstats.max_pkt_size = uint64(pkt.wireLength)
			}
			if xstats.min_pkt_size == 0 || xstats.min_pkt_size > uint64(pkt.wireLength) {
				xstats.min_pkt_size = uint64(pkt.wireLength)
			}
			xstats.avg_pkt_size = xstats.total_bytes / xstats.total_packets

			if xstats.prev_timestamp == 0 {
				xstats.prev_timestamp = stat.Timestamp
			}
			if stat.re_payload_get_timestamp != 0 {
				latency := uint64(0)
				if stat.Timestamp > stat.re_payload_get_timestamp {
					latency = stat.Timestamp - stat.re_payload_get_timestamp
				}
				xstats.latency_wrap = append(xstats.latency_wrap, float64(latency))
				if xstats.max_latency < latency {
					xstats.max_latency = latency
				}
				if xstats.min_latency == 0 || xstats.min_latency > latency {
					xstats.min_latency = latency
				}
				xstats.avg_latency = (xstats.avg_latency + latency) / 2
			}

			xstats.size_pkts_wrap = append(xstats.size_pkts_wrap, float64(pkt.wireLength))
			inter_pkt_delay := stat.Timestamp - xstats.prev_timestamp
			xstats.cur_inter_pkt_delay = inter_pkt_delay
			xstats.delay_pkts_wrap = append(xstats.delay_pkts_wrap, strconv.Itoa(int(inter_pkt_delay)))
			xstats.wrap_inter_pkt_delay = ""
			if xstats.max_inter_pkt_delay < inter_pkt_delay {
				xstats.max_inter_pkt_delay = inter_pkt_delay
			}
			if xstats.min_inter_pkt_delay == 0 || xstats.min_inter_pkt_delay > inter_pkt_delay {
				xstats.min_inter_pkt_delay = inter_pkt_delay
			}
			xstats.avg_inter_pkt_delay = (xstats.avg_inter_pkt_delay + inter_pkt_delay) / 2

			if stat.Eom {
				if xstats.wrap_start_timestamp != 0 {
					xstats.wrap_time = uint64(time.Now().UnixMilli()) - xstats.wrap_start_timestamp
				}
				xstats.percentile_pkt_size = ""
				sort.Float64s(xstats.size_pkts_wrap)
				for i := 10; i <= 90; i += 10 {
					percentileValue := percentile(xstats.size_pkts_wrap, float64(i))
					xstats.percentile_pkt_size += fmt.Sprintf("%d:%f,", i, percentileValue)
				}
				xstats.percentile_pkt_size += fmt.Sprintf("95:%f,99:%f", percentile(xstats.size_pkts_wrap, 95), percentile(xstats.size_pkts_wrap, 99))
				xstats.size_pkts_wrap = []float64{}
				xstats.percentile_latency = ""
				sort.Float64s(xstats.latency_wrap)
				for i := 50; i <= 80; i += 10 {
					percentileValue := percentile(xstats.latency_wrap, float64(i))
					xstats.percentile_latency += fmt.Sprintf("%d:%f,", i, percentileValue)
				}
				xstats.percentile_latency += fmt.Sprintf("85:%f,90:%f,95:%f", percentile(xstats.latency_wrap, 85), percentile(xstats.latency_wrap, 90), percentile(xstats.latency_wrap, 95))
				for i := 96; i <= 100; i++ {
					percentileValue := percentile(xstats.latency_wrap, float64(i))
					xstats.percentile_latency += fmt.Sprintf(",%d:%f", i, percentileValue)
				}
				xstats.latency_wrap = []float64{}
				xstats.wrap_start_timestamp = 0
				xstats.packets_per_wrap = xstats.cur_packets_per_wrap
				xstats.bytes_per_wrap = xstats.cur_bytes_per_wrap
				xstats.cur_packets_per_wrap = 0
				xstats.cur_bytes_per_wrap = 0
				xstats.cur_wrap_inter_pkt_delay = strings.Join(xstats.delay_pkts_wrap[:], ",")
				xstats.wrap_inter_pkt_delay = ""
				xstats.delay_pkts_wrap = []string{}

				xstats.wrap_counter++
			} else if xstats.wrap_start_timestamp == 0 {
				xstats.wrap_start_timestamp = uint64(time.Now().UnixMilli())
			}
			xstats.prev_timestamp = stat.Timestamp

			h.xpathStats[path] = xstats
		}
	}
}

func (h *statshandler) getKPIStats(subResponse *gnmi_pb.SubscribeResponse) *kpiStats {
	var jHdrPresent bool
	stats := new(kpiStats)
	notfn := subResponse.GetUpdate()
	if notfn == nil {
		return nil
	}
	stats.notif_timestamp = notfn.Timestamp
	extns := subResponse.GetExtension()

	if extns != nil {
		var extIds []gnmi_ext1.ExtensionID
		for _, ext := range extns {
			regExtn := ext.GetRegisteredExt()
			if (regExtn.GetId()) != gnmi_ext1.ExtensionID_EID_JUNIPER_TELEMETRY_HEADER {
				extIds = append(extIds, regExtn.GetId())
				continue
			}

			jHdrPresent = true
			var hdr gnmi_juniper_header_ext.GnmiJuniperTelemetryHeaderExtension
			msg := regExtn.GetMsg()
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
			stats.Eom = hdr.Eom

			if hdr.ExportTimestamp > 0 {
				stats.Timestamp = uint64(hdr.ExportTimestamp)
			}
			if hdr.PayloadGetTimestamp > 0 {
				stats.re_payload_get_timestamp = uint64(hdr.PayloadGetTimestamp)
			}
			if hdr.StreamCreationTimestamp > 0 {
				stats.re_stream_creation_timestamp = uint64(hdr.StreamCreationTimestamp)
			}
			break
		}
		if !jHdrPresent {
			jLog(h.jctx, fmt.Sprintf(
				"Juniper header extension not present, available extensions: %v", extIds))
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

var previous_bytes uint64
var previous_packets uint64
var previous_time uint64

// var previous_xpath_initialsync_stats = make(map[string]xpathStats)

var RATE_SAMPLING_INTERVAL_SECS uint64 = 10

// var write_xpath2file bool = false
// var write_file_delay uint64 = 0

func (h *statshandler) printStatsRate() {
	now := time.Now()
	var current_secs uint64 = uint64(now.UnixMilli() / 1000)

	if h.previousSecs == 0 {
		h.previousSecs = current_secs
	}
	// s := fmt.Sprintf("Current Time: %d, Previous Time: %d\n", current_secs, h.previousSecs)
	// fmt.Println(s)
	if current_secs > (h.previousSecs + RATE_SAMPLING_INTERVAL_SECS) {
		secs_diff := current_secs - h.previousSecs

		// Async stats-queue health: how many samples we've dropped because the
		// worker fell behind, and current buffer occupancy. dropped is written
		// on the gRPC receive goroutine, so read it atomically.
		dropped := atomic.LoadUint64(&h.dropped)
		bufLen := len(h.statsCh)
		bufCap := cap(h.statsCh)
		jLog(h.jctx, fmt.Sprintf("stats-handler queue: dropped=%d buffer=%d/%d", dropped, bufLen, bufCap))
		publishKPIToInflux(h.jctx, "kpi-stats-handler",
			map[string]string{"host": h.jctx.config.Host},
			map[string]interface{}{
				"dropped_total": int64(dropped),
				"buffer_len":    int64(bufLen),
				"buffer_cap":    int64(bufCap),
			})

		// hs := fmt.Sprintf("Total Header Bytes: %d", jctx.stats.totalInHeaderWireLength)
		// fmt.Println(hs)
		// ts := fmt.Sprintf("Total Pyaload Bytes: %d, Total Packets: %d\n", jctx.stats.totalInPayloadWireLength, jctx.stats.totalIn)
		// fmt.Println(ts)

		// total_bytes := (jctx.stats.totalInPayloadWireLength + jctx.stats.totalInHeaderWireLength) - previous_bytes
		// total_packets := jctx.stats.totalIn - previous_packets

		// s := fmt.Sprintf("Bytes/sec: %d, Packets/sec: %d\n", total_bytes/secs_diff, total_packets/secs_diff)
		// fmt.Println(s)
		// previous_bytes = (jctx.stats.totalInPayloadWireLength + jctx.stats.totalInHeaderWireLength)
		// previous_packets = jctx.stats.totalIn
		for k, v := range h.xpathStats {
			// fmt.Println("xpath_stats: ", k, v)
			if _, exists := h.previousXpathStats[k]; !exists {
				h.previousXpathStats[k] = xpathStats{
					total_bytes:          0,
					total_packets:        0,
					max_pkt_size:         0,
					min_pkt_size:         0,
					avg_pkt_size:         0,
					max_latency:          0,
					min_latency:          0,
					avg_latency:          0,
					max_inter_pkt_delay:  0,
					min_inter_pkt_delay:  0,
					avg_inter_pkt_delay:  0,
					prev_timestamp:       0,
					wrap_time:            0,
					wrap_start_timestamp: 0,
					Eos:                  false,
				}
			}
			pv := h.previousXpathStats[k]
			if pv.total_packets == v.total_packets {
				continue
			}
			// fmt.Println("previous xpath_stats: ", k, pv)
			//path, bytes, bytes/sec
			// fmt.Printf(
			// 	fmt.Sprintf("%s,%d,%d,%d,%d,%d\n", k, (v.total_bytes - pv.total_bytes), (v.total_bytes-pv.total_bytes)/secs_diff,
			// 		v.max_pkt_size, v.min_pkt_size, v.avg_pkt_size))

			tags := map[string]string{
				"sensor_info": k,
			}
			fields := map[string]interface{}{
				"cur_sequence_number":   int64(v.sequence_number),
				"total_bytes":           int64(v.total_bytes),
				"total_packets":         int64(v.total_packets),
				"bytes_per_sec":         int64((v.total_bytes - pv.total_bytes) / secs_diff),
				"packets_per_sec":       int64((v.total_packets - pv.total_packets) / secs_diff),
				"max_pkt_size":          int64(v.max_pkt_size),
				"min_pkt_size":          int64(v.min_pkt_size),
				"avg_pkt_size":          int64(v.avg_pkt_size),
				"max_latency":           int64(v.max_latency),
				"min_latency":           int64(v.min_latency),
				"avg_latency":           int64(v.avg_latency),
				"max_inter_pkt_delay":   int64(v.max_inter_pkt_delay),
				"min_inter_pkt_delay":   int64(v.min_inter_pkt_delay),
				"avg_inter_pkt_delay":   int64(v.avg_inter_pkt_delay),
				"wrap_time":             int64(v.wrap_time),
				"packets_per_wrap":      int64(v.packets_per_wrap),
				"bytes_per_wrap":        int64(v.bytes_per_wrap),
				"initial_drop_counter":  int64(v.initial_drop_counter),
				"periodic_drop_counter": int64(v.periodic_drop_counter),
				"wrap_counter":          int64(v.wrap_counter),
				"wrap_inter_pkt_delay":  v.cur_wrap_inter_pkt_delay,
				"percentile_pkt_size":   v.percentile_pkt_size,
				"percentile_latency":    v.percentile_latency,
				"Eos":                   v.Eos,
			}
			publishKPIToInflux(h.jctx, "kpi-measurements", tags, fields)
			h.previousXpathStats[k] = v
		}
		h.previousSecs = current_secs

		// write_file_delay++
		// }
		/*
			// if write_file_delay % 2 {
			xpath_file := new(os.File)
			defer xpath_file.Close()
			xpath_zero_file := new(os.File)
			defer xpath_zero_file.Close()

			xpath_file, err := os.Create("re_xpaths.log")
			if err != nil {
				log.Fatalf("Couldn't create log file:", err)
			}
			xpath_zero_file, err = os.Create("re_xpaths_zero.log")
			if err != nil {
				log.Fatalf("Couldn't create log file:", err)
			}

			keys := make([]string, 0, len(re_xpaths))
			for k := range re_xpaths {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				flag := "false"
				if re_xpaths[k].is_zero {
					flag = "true"
				}
				xpath_file.Write([]byte(fmt.Sprintf("%v, %d, %d, %d, %d, %s\n", k, re_xpaths[k].prev_timestamp, re_xpaths[k].prev_exp_timestamp,
					re_xpaths[k].curr_timestamp, re_xpaths[k].curr_exp_timestamp, flag)))
			}

			xpath_file, err = os.Create("lc_xpaths.log")
			if err != nil {
				log.Fatalf("Couldn't create log file:", err)
			}
			xpath_zero_file, err = os.Create("lc_xpaths_zero.log")
			if err != nil {
				log.Fatalf("Couldn't create log file:", err)
			}
			keys = make([]string, 0, len(lc_xpaths))
			for k := range lc_xpaths {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			for _, k := range keys {
				flag := "false"
				if lc_xpaths[k].is_zero {
					flag = "true"
				}
				xpath_file.Write([]byte(fmt.Sprintf("%v, %d, %d, %d, %d, %s\n", k, lc_xpaths[k].prev_timestamp, lc_xpaths[k].prev_exp_timestamp,
					lc_xpaths[k].curr_timestamp, lc_xpaths[k].curr_exp_timestamp, flag)))
			}

			xpath_file, err = os.Create("lc_xpaths_leaves.log")
			if err != nil {
				log.Fatalf("Couldn't create log file:", err)
			}
			xpath_zero_file, err = os.Create("lc_xpaths_leaves_zero.log")
			if err != nil {
				log.Fatalf("Couldn't create log file:", err)
			}
			var xpaths_unique_leaves = make(map[string]int)

			for key, value := range lc_xpaths_leaves {
				splits := strings.Split(key, "/")
				key = splits[len(splits)-1]
				xpaths_unique_leaves[key] = value
			}
			for key, value := range xpaths_unique_leaves {
				if value != 0 {
					xpath_file.Write([]byte(fmt.Sprintf("%v\n", key)))
				} else {
					xpath_zero_file.Write([]byte(fmt.Sprintf("%v\n", key)))
				}
			}
		*/
		// xpath_file, err = os.Create("re_xpaths_leaves.log")
		// if err != nil {
		// 	log.Fatalf("Couldn't create log file:", err)
		// }
		// xpath_zero_file, err = os.Create("re_xpaths_leaves_zero.log")
		// if err != nil {
		// 	log.Fatalf("Couldn't create log file:", err)
		// }

		// for key, value := range re_xpaths_leaves {
		// 	if value != 0 {
		// 		xpath_file.Write([]byte(fmt.Sprintf("%v: %v\n", key, value)))
		// 	} else {
		// 		xpath_zero_file.Write([]byte(fmt.Sprintf("%v: %v\n", key, value)))
		// 	}
		// }

		// write_xpath2file = true
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

	// Print Summary to terminal for internal Jitmon
	if jctx.config.InternalJtimon.CsvLog != "" {
		fmt.Println(s)
	}
}

func isCsvStatsEnabled(jctx *JCtx) bool {
	if *stateHandler && jctx.config.InternalJtimon.CsvLog != "" {
		return true
	}
	return false
}

func csvStatsLogInit(jctx *JCtx) {
	if !*stateHandler && jctx.config.InternalJtimon.CsvLog == "" {
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
