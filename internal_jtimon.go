package main

import (
	"fmt"
	"log"
	"os"

	na_pb "github.com/Juniper/jtimon/telemetry"
)

// InternalJtimonConfig type
type InternalJtimonConfig struct {
	DataLog string `json:"data-log-file"`
	out     *os.File
	logger  *log.Logger
}

func internalJtimonLogInit(jctx *JCtx) {
	if jctx.config.InternalJtimon.DataLog == "" {
		return
	}
	var out *os.File

	var err error
	out, err = os.OpenFile(jctx.config.InternalJtimon.DataLog, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		log.Printf("Could not create internal jtimon log file(%s): %v\n", jctx.config.InternalJtimon.DataLog, err)
	}

	if out != nil {
		flags := 0

		jctx.config.InternalJtimon.logger = log.New(out, "", flags)
		jctx.config.InternalJtimon.out = out

		log.Printf("logging in %s for %s:%d [in the format of internal jtimon tool]\n",
			jctx.config.InternalJtimon.DataLog, jctx.config.Host, jctx.config.Port)
	}
}

func internalJtimonLogStop(jctx *JCtx) {
	if jctx.config.InternalJtimon.out != nil {
		jctx.config.InternalJtimon.out.Close()
		jctx.config.InternalJtimon.out = nil
		jctx.config.InternalJtimon.logger = nil
	}
}

func isInternalJtimonLogging(jctx *JCtx) bool {
	return jctx.config.InternalJtimon.logger != nil
}

func jLogInternalJtimonForGnmi(jctx *JCtx, parseOutput *gnmiParseOutputT) {
	if jctx.config.InternalJtimon.logger == nil {
		return
	}

	// Log here in the format of internal jtimon
	var (
		jxpaths  map[string]interface{}
		jGnmiHdr string
	)

	// s := ""
	// s += fmt.Sprintf("system_id: %s\n", parseOutput.jHeader.hdr)
	// s += fmt.Sprintf("component_id: %d\n", ocData.ComponentId)
	// s += fmt.Sprintf("sub_component_id: %d\n", ocData.SubComponentId)
	// s += fmt.Sprintf("path: %s\n", ocData.Path)
	// s += fmt.Sprintf("sequence_number: %d\n", ocData.SequenceNumber)
	// s += fmt.Sprintf("timestamp: %d\n", ocData.Timestamp)
	// s += fmt.Sprintf("sync_response: %v\n", ocData.SyncResponse)

	jctx.config.InternalJtimon.logger.Printf(fmt.Sprintf("prefix: %v, kvpairs: %v, xpathVal: %v, juniperXpathVal: %v, "+
		"juniperhdr: %v, measurement: %v \n\n", parseOutput.prefixPath, parseOutput.kvpairs,
		parseOutput.xpaths, jxpaths, jGnmiHdr, parseOutput.mName))
}

func jLogInternalJtimonForPreGnmi(jctx *JCtx, ocdata *na_pb.OpenConfigData) {
	if jctx.config.InternalJtimon.logger == nil {
		return
	}

	// Log here in the format of internal jtimon
	jctx.config.InternalJtimon.logger.Printf("Vivek.. %v", *ocdata)
}
