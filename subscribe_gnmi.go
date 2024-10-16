package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/Juniper/jtimon/dialout"
	gnmi "github.com/Juniper/jtimon/gnmi/gnmi"
	google_protobuf "github.com/golang/protobuf/ptypes/any"
	"github.com/influxdata/influxdb/client/v2"
	"golang.org/x/net/context"

	"google.golang.org/grpc"
)

// Only for unit test and coverage purposes
var gGnmiUnitTestCoverage bool

/*
 * Publish metrics to Prometheus. Below is the terminology:
 *   1. Field - Metric
 *   2. Tags - Labels
 */
func publishToPrometheus(jctx *JCtx, parseOutput *gnmiParseOutputT) {
	var (
		promKvpairs = map[string]string{}
		alias       = jctx.alias
	)

	for k, v := range parseOutput.kvpairs {
		promKvpairs[promName(getAlias(alias, k))] = v
	}

	for p, v := range parseOutput.xpaths {
		splits := strings.Split(p, gXPathTokenPathSep)
		if strings.HasPrefix(splits[len(splits)-1], gGnmiJuniperInternalFieldsPrefix) {
			continue
		}

		var floatVal float64
		switch v.(type) {
		case int64:
			floatVal = float64(v.(int64))
		case float64:
			floatVal = v.(float64)
		case bool:
			if v.(bool) == true {
				floatVal = 1
			} else {
				floatVal = 0
			}
		case string:
			// store leaf of type string as the tag
			leafStr := convertToTag(p)
			if _, ok := promKvpairs[promName(getAlias(alias, leafStr))]; !ok {
				promKvpairs[promName(getAlias(alias, leafStr))] = v.(string)
			} else {
				continue // do not insert existing tag
			}
			floatVal = 0 // insert the leaf with 0 as float value
		case *google_protobuf.Any:
		case []interface{}:
		case []byte:
		default:
			jLog(jctx, fmt.Sprintf("Unsupported type %T for %v", v, p))
			continue
		}

		metric := &jtimonMetric{
			metricName:       promName(getAlias(alias, p)),
			metricExpiration: time.Now(),
			metricValue:      floatVal,
			metricLabels:     promKvpairs,
		}

		metric.mapKey = getMapKey(metric)

		if *print || IsVerboseLogging(jctx) {
			jLog(jctx, fmt.Sprintf("metricName: %v, metricValue: %v, metricLabels: %v, mapKey: %v \n", metric.metricName, metric.metricValue, metric.metricLabels, metric.mapKey))
		}

		if !gGnmiUnitTestCoverage {
			exporter.ch <- metric
		}
	}

	return
}

/*
 * Publish parsed output to Influx. Make sure there are only integers,
 * floats and strings. Influx Line Protocol doesn't support other types
 */
func publishToInflux(jctx *JCtx, mName string, prefixPath string, kvpairs map[string]string, xpaths map[string]interface{}) error {
	if !gGnmiUnitTestCoverage && jctx.influxCtx.influxClient == nil {
		return nil
	}

	// Convert leaf-list values if present in xpaths for influxdb Point write
	for key, value := range xpaths {
		// Check if the value is a slice
		if members, ok := value.([]string); ok {
			// Join the slice elements into a single string separated by commas
			xpaths[key] = strings.Join(members, ",")
		}
	}

	pt, err := client.NewPoint(mName, kvpairs, xpaths, time.Now())
	if err != nil {
		msg := fmt.Sprintf("New point creation failed for (key: %v, xpaths: %v): %v", kvpairs, xpaths, err)
		jLog(jctx, msg)
		return errors.New(msg)
	}

	jLog(jctx, fmt.Sprintf("jctx.config.Influx.WritePerMeasurement: %v.. ", jctx.config.Influx.WritePerMeasurement))
	if jctx.config.Influx.WritePerMeasurement {
		if *print || IsVerboseLogging(jctx) {
			msg := fmt.Sprintf("New point (per measurement): %v", pt.String())
			jLog(jctx, msg)
		}

		if !gGnmiUnitTestCoverage {
			jctx.influxCtx.batchWMCh <- &batchWMData{
				measurement: mName,
				points:      []*client.Point{pt},
			}
		}
	} else {
		if *print || IsVerboseLogging(jctx) {
			msg := fmt.Sprintf("New point: %v", pt.String())
			jLog(jctx, msg)
		}

		if !gGnmiUnitTestCoverage {
			jctx.influxCtx.batchWCh <- []*client.Point{pt}
		}
	}

	return nil
}

/*
 * Extract the following from gNMI response and already parsed output:
 *   1. Juniper telemetry header, if it is a Juniper packet
 *   2. Value for the tag "sensor"
 *   3. Measuremnet name
 *   4. Timestamps:
 *        a) Producer timestamp
 *        b) Export timestamp - Only available in Juniper packet, the time at which device(?? actually NA) published
 */
func gnmiParseHeader(rsp *gnmi.SubscribeResponse, parseOutput *gnmiParseOutputT) (*gnmiParseOutputT, error) {
	var (
		juniperHdrDetails *juniperGnmiHeaderDetails
		ok                bool
		err               error

		verboseSensorDetails, mName string
	)

	prefixPath := parseOutput.prefixPath
	jXpaths := parseOutput.jXpaths
	xpathVal := parseOutput.xpaths

	/*
	 * Identify the measurement name, default is prefix path. For juniper packets that have headers,
	 * it will get overridden with subscribed path if it is present in the header.
	 */
	mName = prefixPath + gXPathTokenPathSep // To be compatible with that of OC
	// Try using the proper gnmi_ext.proto's path in gnmi.proto, now it is manually edited
	juniperHdrDetails, ok, err = formJuniperTelemetryHdr(jXpaths, rsp.GetExtension())
	if !ok {
		// Not a juniper packet, take prefix as the path subscribed
		ps := rsp.GetUpdate().GetTimestamp()
		// Specifically added for Cisco, the device sends spurious messages with timestamp 0
		if ps == 0 {
			errMsg := fmt.Sprintf("Invalid message, producer timestamp is 0")
			return nil, errors.New(errMsg)
		}
		parseOutput.sensorVal = prefixPath
		parseOutput.mName = mName // To be compatible with that of OC
		tsInMillisecs := (ps / gGnmiFreqToMilli)
		xpathVal[prefixPath+gXPathTokenPathSep+gGnmiJtimonProducerTsName] = tsInMillisecs
		xpathVal[gGnmiJtimonDeviceTsName] = tsInMillisecs
		//fmt.Printf("Vivek.. parseOuput 1: %v", parseOutput)
		return parseOutput, nil
	}

	if err != nil {
		//fmt.Printf("Vivek.. parseOuput 2: %v", parseOutput)
		return parseOutput, err
	}

	if juniperHdrDetails.hdr != nil {
		var hdr = juniperHdrDetails.hdr
		verboseSensorDetails = hdr.GetPath()
		splits := strings.Split(verboseSensorDetails, gGnmiVerboseSensorDetailsDelim)

		if splits[2] != "" {
			mName = splits[2] // Denotes subscribed path
		}
		if jXpaths.publishTsXpath != "" {
			xpathVal[prefixPath+gXPathTokenPathSep+gGnmiJtimonExportTsName] = jXpaths.xPaths[jXpaths.publishTsXpath]
		}

		tsInMillisecs := (rsp.GetUpdate().GetTimestamp() / gGnmiFreqToMilli)
		xpathVal[prefixPath+gXPathTokenPathSep+gGnmiJtimonProducerTsName] = tsInMillisecs
		xpathVal[gGnmiJtimonDeviceTsName] = tsInMillisecs
	} else {
		var hdr = juniperHdrDetails.hdrExt
		verboseSensorDetails = hdr.GetSensorName() + gGnmiVerboseSensorDetailsDelim +
			hdr.GetStreamedPath() + gGnmiVerboseSensorDetailsDelim +
			hdr.GetSubscribedPath() + gGnmiVerboseSensorDetailsDelim +
			hdr.GetComponent()

		if hdr.GetSubscribedPath() != "" {
			mName = hdr.GetSubscribedPath()
		}
		xpathVal[prefixPath+gXPathTokenPathSep+gGnmiJtimonExportTsName] = hdr.GetExportTimestamp()

		tsInMillisecs := (rsp.GetUpdate().GetTimestamp() / gGnmiFreqToMilli)
		xpathVal[prefixPath+gXPathTokenPathSep+gGnmiJtimonProducerTsName] = tsInMillisecs
		xpathVal[gGnmiJtimonDeviceTsName] = tsInMillisecs
	}

	parseOutput.jHeader = juniperHdrDetails
	parseOutput.sensorVal = verboseSensorDetails
	parseOutput.mName = mName
	return parseOutput, nil
}

/*
 * Extract the following from gNMI response and already parsed output:
 *   1. Tags aka kvpairs
 *   2. Fields aka xpaths
 *   3. Juniper telemery header, "sensor" value and measurement name
 */
func gnmiParseNotification(parseOrigin bool, rsp *gnmi.SubscribeResponse, parseOutput *gnmiParseOutputT, enableUint bool) (*gnmiParseOutputT, error) {
	var (
		errMsg string
		err    error
	)

	notif := rsp.GetUpdate()
	if notif == nil {
		errMsg = fmt.Sprintf("Not any of error/sync/update !!")
		return parseOutput, errors.New(errMsg)
	}

	if len(notif.GetUpdate()) != 0 {
		parseOutput, err = gnmiParseUpdates(parseOrigin, notif.GetPrefix(), notif.GetUpdate(), parseOutput, enableUint)
		if err != nil {
			errMsg = fmt.Sprintf("gnmiParseUpdates failed: %v", err)
			return parseOutput, errors.New(errMsg)
		}
	}

	if len(notif.GetDelete()) != 0 {
		parseOutput, err = gnmiParseDeletes(parseOrigin, notif.GetPrefix(), notif.GetDelete(), parseOutput)
		if err != nil {
			return parseOutput, err
		}
	}

	/*
	 * Update in-kvs immediately after we form xpaths from the rsp because
	 * down the line xpaths will get updated with additional jtimon specific
	 * fields to be written to influx
	 */
	parseOutput.inKvs += uint64(len(parseOutput.xpaths))
	if parseOutput.jXpaths != nil {
		parseOutput.inKvs += uint64(len(parseOutput.jXpaths.xPaths))
	}

	parseOutput, err = gnmiParseHeader(rsp, parseOutput)
	if err != nil {
		errMsg = fmt.Sprintf("gnmiParseHeader failed: %v", err)
		return parseOutput, errors.New(errMsg)
	}

	return parseOutput, nil
}

/*
 * Parse gNMI response and publish to Influx and Prometheus
 */
func gnmiHandleResponse(jctx *JCtx, rsp *gnmi.SubscribeResponse) error {
	var (
		tmpParseOp  = gnmiParseOutputT{kvpairs: map[string]string{}, xpaths: map[string]interface{}{}}
		parseOutput = &tmpParseOp
		err         error

		eosEnabled = false
		hostname   = jctx.config.Host + ":" + strconv.Itoa(jctx.config.Port)
	)

	// Update packet stats
	updateStats(jctx, nil, true)
	if syncRsp := rsp.GetSyncResponse(); syncRsp {
		jLog(jctx, fmt.Sprintf("gNMI host: %v, received sync response", hostname))
		parseOutput.syncRsp = true
		jctx.receivedSyncRsp = true
		return nil
	}

	/*
	 * Extract prefix, tags, values and juniper speecific header info if present
	 */
	parseOutput, err = gnmiParseNotification(!jctx.config.Vendor.RemoveNS, rsp, parseOutput, jctx.config.EnableUintSupport)
	if err != nil {
		jLog(jctx, fmt.Sprintf("gNMI host: %v, parsing notification failed: %v", hostname, err.Error()))
		return err
	}

	// Update kv stats
	updateStatsKV(jctx, true, parseOutput.inKvs)

	// Ignore all packets till sync response is received.
	eosEnabled = jctx.config.EOS
	if isInternalJtimonLogging(jctx) {
		eosEnabled = jctx.config.InternalJtimon.GnmiEOS
	}
	if !eosEnabled {
		if !jctx.receivedSyncRsp {
			if parseOutput.jHeader != nil {
				// For juniper packets, ignore only the packets which are numbered in initial sync sequence range
				if parseOutput.jHeader.hdr != nil {
					if parseOutput.jHeader.hdr.GetSequenceNumber() >= gGnmiJuniperIsyncSeqNumBegin &&
						parseOutput.jHeader.hdr.GetSequenceNumber() <= gGnmiJuniperIsyncSeqNumEnd {
						errMsg := fmt.Sprintf("%s. Dropping initial sync packet, seq num: %v", gGnmiJtimonIgnoreErrorSubstr, parseOutput.jHeader.hdr.GetSequenceNumber())
						return errors.New(errMsg)
					}
				}

				if parseOutput.jHeader.hdrExt != nil {
					if parseOutput.jHeader.hdrExt.GetSequenceNumber() >= gGnmiJuniperIsyncSeqNumBegin &&
						parseOutput.jHeader.hdrExt.GetSequenceNumber() <= gGnmiJuniperIsyncSeqNumEnd {
						errMsg := fmt.Sprintf("%s. Dropping initial sync packet, seq num: %v", gGnmiJtimonIgnoreErrorSubstr, parseOutput.jHeader.hdrExt.GetSequenceNumber())
						return errors.New(errMsg)
					}
				}
			} else {
				errMsg := fmt.Sprintf("%s. Dropping initial sync packet", gGnmiJtimonIgnoreErrorSubstr)
				return errors.New(errMsg)
			}
		}
	}

	if parseOutput.mName == "" {
		jLog(jctx, fmt.Sprintf("gNMI host: %v, measurement name extraction failed", hostname))
		return errors.New("Measurement name extraction failed")
	}

	parseOutput.kvpairs["device"] = jctx.config.Host
	parseOutput.kvpairs["sensor"] = parseOutput.sensorVal
	parseOutput.xpaths["vendor"] = "gnmi"

	if *prom {
		if *noppgoroutines {
			publishToPrometheus(jctx, parseOutput)
		} else {
			go publishToPrometheus(jctx, parseOutput)
		}
	}

	if *print || IsVerboseLogging(jctx) {
		var (
			jxpaths  map[string]interface{}
			jGnmiHdr string
		)

		if parseOutput.jXpaths != nil {
			jxpaths = parseOutput.jXpaths.xPaths
		}
		if parseOutput.jHeader != nil {
			if parseOutput.jHeader.hdr != nil {
				jGnmiHdr = "updates header{" + parseOutput.jHeader.hdr.String() + "}"
			} else {
				jGnmiHdr = "extension header{" + parseOutput.jHeader.hdrExt.String() + "}"
				var jHeaderData map[string]interface{}
				jGnmiHdrExt, err := json.Marshal(parseOutput.jHeader.hdrExt)
				if err != nil {
					return errors.New("unable to Marshal Juniper extension header")
				}
				err = json.Unmarshal(jGnmiHdrExt, &jHeaderData)
				if err != nil {
					return errors.New("unable to decode Juniper extension header")
				}
				jHeaderKeyToTags := []string{"component", "component_id", "sub_component_id"}
				for _, v := range jHeaderKeyToTags {
					if _, ok := jHeaderData[v]; ok {
						strVal := convertToString(jHeaderData[v])
						if strVal == "Unsupported type" {
							jLog(jctx, fmt.Sprintf(".Skip Adding juniper Header Extension: %s "+
								"to Tags. Unable to convert extension value: %v to string. ", v, jHeaderData[v]))
							continue
						}
						parseOutput.kvpairs[v] = strVal
					}
				}
			}

		}

		jLog(jctx, fmt.Sprintf("prefix: %v, kvpairs: %v, xpathVal: %v, juniperXpathVal: %v, juniperhdr: %v, measurement: %v, rsp: %v\n\n",
			parseOutput.prefixPath, parseOutput.kvpairs, parseOutput.xpaths, jxpaths, jGnmiHdr, parseOutput.mName, rsp))
	}

	internalJtimonEnabled := isInternalJtimonLogging(jctx)
	if internalJtimonEnabled {
		jLogInternalJtimonForGnmi(jctx, parseOutput, rsp)
	}

	err = publishToInflux(jctx, parseOutput.mName, parseOutput.prefixPath, parseOutput.kvpairs, parseOutput.xpaths)
	if err != nil {
		jLog(jctx, fmt.Sprintf("Publish to Influx fails: %v\n\n", parseOutput.mName))
		return err
	}

	return err
}

// Convert xpaths config to gNMI subscription
func xPathsTognmiSubscription(pathsCfg []PathsConfig, dialOutpathsCfg []*dialout.PathsConfig) ([]*gnmi.Subscription, error) {
	var subs []*gnmi.Subscription

	if dialOutpathsCfg == nil {
		for _, p := range pathsCfg {
			gp, err := xPathTognmiPath(p.Path)
			if err != nil {
				return nil, err
			}

			mode := gnmiMode(p.Mode)
			mode, freq := gnmiFreq(mode, p.Freq)
			gp.Origin = p.Origin

			subs = append(subs, &gnmi.Subscription{Path: gp, Mode: mode, SampleInterval: freq})
		}
	} else {
		for _, p := range dialOutpathsCfg {
			gp, err := xPathTognmiPath(p.Path)
			if err != nil {
				return nil, err
			}

			mode := gnmiMode(p.Mode)
			mode, freq := gnmiFreq(mode, p.Frequency)

			subs = append(subs, &gnmi.Subscription{Path: gp, Mode: mode, SampleInterval: freq})
		}
	}

	return subs, nil
}

// subscribe routine constructs the subscription paths and calls
// the function to start the streaming connection.
//
// In case of SIGHUP, the paths are formed again and streaming
// is restarted.
func subscribegNMI(conn *grpc.ClientConn, jctx *JCtx, cfg Config, paths []PathsConfig) SubErrorCode {
	var (
		subs gnmi.SubscriptionList
		sub  = gnmi.SubscribeRequest_Subscribe{Subscribe: &subs}
		req  = gnmi.SubscribeRequest{Request: &sub}
		err  error

		hostname = jctx.config.Host + ":" + strconv.Itoa(jctx.config.Port)
		ctx      context.Context
	)

	// 1. Form request

	// Support only STREAM
	subs.Mode = gnmi.SubscriptionList_STREAM

	// PROTO encoding
	if jctx.config.Vendor.Gnmi != nil {
		switch jctx.config.Vendor.Gnmi.Encoding {
		case "json":
			subs.Encoding = gnmi.Encoding_JSON
		case "json_ietf":
			subs.Encoding = gnmi.Encoding_JSON_IETF
		default:
			subs.Encoding = gnmi.Encoding_PROTO
		}
	}

	// Form subscription from xpaths config
	subs.Subscription, err = xPathsTognmiSubscription(paths, nil)
	if err != nil {
		jLog(jctx, fmt.Sprintf("gNMI host: %v, Invalid path: %v", hostname, err))
		// To make worker absorb any further config changes
		return SubRcConnRetry
	}

	// 2. Subscribe
	if jctx.config.User != "" && jctx.config.Password != "" {
		md := metadata.New(map[string]string{"username": jctx.config.User, "password": jctx.config.Password})
		ctx = metadata.NewOutgoingContext(context.Background(), md)
	} else {
		ctx = context.Background()
	}
	gNMISubHandle, err := gnmi.NewGNMIClient(conn).Subscribe(ctx)
	if err != nil {
		jLog(jctx, fmt.Sprintf("gNMI host: %v, subscribe handle creation failed, err: %v", hostname, err))
		return SubRcConnRetry
	}

	err = gNMISubHandle.Send(&req)
	if err != nil {
		jLog(jctx, fmt.Sprintf("gNMI host: %v, send request failed: %v", hostname, err))
		return SubRcConnRetry
	}

	datach := make(chan SubErrorCode)

	// 3. Receive rsp
	go func() {
		var (
			rsp  *gnmi.SubscribeResponse
			err1 error
		)

		jLog(jctx, fmt.Sprintf("gNMI host: %v, receiving data..", hostname))
		for {
			rsp, err1 = gNMISubHandle.Recv()
			if err1 == io.EOF {
				printSummary(jctx)
				jLog(jctx, fmt.Sprintf("gNMI host: %v, received eof", hostname))
				datach <- SubRcConnRetry
				return
			}

			if err1 != nil {
				jLog(jctx, fmt.Sprintf("gNMI host: %v, receive response failed: %v", hostname, err1))
				sc, sErr := status.FromError(err)
				if !sErr {
					jLog(jctx, fmt.Sprintf("Failed to retrieve status from error: %v", sErr))
					datach <- SubRcConnRetry
					return
				}
				/*
				 * Unavailable is just a cover-up for JUNOS, ideally the device is expected to return:
				 *   1. Unimplemented if RPC is not available yet
				 *   2. InvalidArgument is RPC is not able to honour the input
				 */
				if sc.Code() == codes.Unimplemented || sc.Code() == codes.InvalidArgument || sc.Code() == codes.Unavailable {
					datach <- SubRcRPCFailedNoRetry
					return
				}

				datach <- SubRcConnRetry
				return
			}

			if *noppgoroutines {
				gnmiErr := gnmiHandleResponse(jctx, rsp)
				if gnmiErr != nil && strings.Contains(gnmiErr.Error(), gGnmiJtimonIgnoreErrorSubstr) {
					jLog(jctx, fmt.Sprintf("gNMI host: %v, parsing response failed: %v", hostname, gnmiErr))
					continue
				}
			} else {
				go func() {
					gnmiErr1 := gnmiHandleResponse(jctx, rsp)
					if gnmiErr1 != nil && strings.Contains(gnmiErr1.Error(), gGnmiJtimonIgnoreErrorSubstr) {
						jLog(jctx, fmt.Sprintf("gNMI host: %v, parsing response failed: %v", hostname, gnmiErr1))
					}
				}()
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
		case subCode := <-datach:
			// return the subcode, proper action will be taken by caller
			return subCode
		}
	}
}
