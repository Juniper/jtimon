package main

import (
	"encoding/json"
	"fmt"
	gnmi "github.com/Juniper/jtimon/gnmi/gnmi"
	na_pb "github.com/Juniper/jtimon/telemetry"
	"log"
	"os"
	"regexp"
	"strings"
)

// InternalJtimonConfig type
type InternalJtimonConfig struct {
	DataLog       string `json:"data-log-file"`
	CsvLog        string `json:"csv-log-file"`
	out           *os.File
	preGnmiOut    *os.File
	csvOut        *os.File
	logger        *log.Logger
	preGnmiLogger *log.Logger
	csvLogger     *log.Logger
	GnmiEOS       bool `json:"gnmi-eos"`
	PreGnmiEOS    bool `json:"pre-gnmi-eos"`
}

func initInternalJtimon(jctx *JCtx) {
	// if Internal Jtimon EOS value is not set,
	// then take the EOS value from parent config
	if !jctx.config.InternalJtimon.GnmiEOS {
		jctx.config.InternalJtimon.GnmiEOS = jctx.config.EOS
	}
	if !jctx.config.InternalJtimon.PreGnmiEOS {
		jctx.config.InternalJtimon.PreGnmiEOS = jctx.config.EOS
	}
}

func internalJtimonLogInit(jctx *JCtx) {
	if jctx.config.InternalJtimon.DataLog == "" {
		return
	}
	var out *os.File

	var err error
	// Gnmi
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

	// Pre-gnmi
	var outPreGnmi *os.File
	outPreGnmi, err = os.OpenFile(fmt.Sprintf("%s_pre-gnmi", jctx.config.InternalJtimon.DataLog), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		log.Printf("Could not create internal jtimon log file(%s_pre-gnmi): %v\n", jctx.config.InternalJtimon.DataLog, err)
	}

	if outPreGnmi != nil {
		flags := 0

		jctx.config.InternalJtimon.preGnmiLogger = log.New(outPreGnmi, "", flags)
		jctx.config.InternalJtimon.preGnmiOut = outPreGnmi

		log.Printf("logging in %s_pre-gnmi for %s:%d [in the format of internal jtimon tool]\n",
			jctx.config.InternalJtimon.DataLog, jctx.config.Host, jctx.config.Port)
	}

	if *stateHandler && jctx.config.InternalJtimon.CsvLog != "" {
		csvStatsLogInit(jctx)
	}
}

func internalJtimonLogStop(jctx *JCtx) {
	if jctx.config.InternalJtimon.out != nil {
		jctx.config.InternalJtimon.out.Close()
		jctx.config.InternalJtimon.out = nil
		jctx.config.InternalJtimon.logger = nil
	}
	if jctx.config.InternalJtimon.preGnmiOut != nil {
		jctx.config.InternalJtimon.preGnmiOut.Close()
		jctx.config.InternalJtimon.preGnmiOut = nil
		jctx.config.InternalJtimon.preGnmiLogger = nil
	}
	if *stateHandler && jctx.config.InternalJtimon.CsvLog != "" {
		csvStatsLogStop(jctx)
	}
}

func isInternalJtimonLogging(jctx *JCtx) bool {
	return jctx.config.InternalJtimon.logger != nil
}

func getPath(prefixPath string, pathElements []*gnmi.PathElem) string {
	for _, pe := range pathElements {
		peName := pe.GetName()
		prefixPath += gXPathTokenPathSep + peName
		is_key := false
		for k, v := range pe.GetKey() {
			if is_key {
				prefixPath += " and " + k + "='" + v + "'"
			} else {
				prefixPath += "[" + k + "='" + v + "'"
				is_key = true
			}
		}
		if is_key {
			prefixPath += "]"
		}
	}

	return prefixPath
}

func jLogInternalJtimonForGnmi(jctx *JCtx, parseOutput *gnmiParseOutputT, rsp *gnmi.SubscribeResponse) {
	if jctx.config.InternalJtimon.logger == nil || parseOutput.jHeader == nil {
		return
	}

	// Log here in the format of internal jtimon
	//var (
	//	jxpaths  map[string]interface{}
	//	jGnmiHdr string
	//)

	s := ""
	//if parseOutput.jHeader.hdr != nil {
	//	s += fmt.Sprintf("system_id: %s\n", parseOutput.jHeader.hdr.String())
	//} else {
	//	s += fmt.Sprintf("system_id: %s\n", parseOutput.jHeader.hdrExt.String())
	//}
	var jHeaderData map[string]interface{}
	jGnmiHdrExt, err := json.Marshal(parseOutput.jHeader.hdrExt)
	if err != nil {
		jLog(jctx, "jLogInternalJtimonForGnmi: unable to Marshal Juniper extension header")
		return
	}
	err = json.Unmarshal(jGnmiHdrExt, &jHeaderData)
	if err != nil {
		jLog(jctx, "jLogInternalJtimonForGnmi: unable to decode Juniper extension header")
		return
	}

	outJHeaderKeys := []string{
		"system_id",
		"component_id",
		"sensor_name",
		"subscribed_path",
		"streamed_path",
		"component",
		"sequence_number",
		"export_timestamp",
	}
	for _, v := range outJHeaderKeys {
		if _, ok := jHeaderData[v]; ok {
			strVal := convertToString(jHeaderData[v])
			if strVal == "Unsupported type" {
				jLog(jctx, fmt.Sprintf(".Skip Adding juniper Header Extension: %s "+
					"to Streamed path. Unable to convert extension value: %v to string. ", v, jHeaderData[v]))
				continue
			}
			s += fmt.Sprintf("%s: %s\n", v, strVal)
		}
	}

	notif := rsp.GetUpdate()
	if notif != nil {
		// Form an xpath for prefix here, as the internal jtimon tool does it this way.
		prefixPath := ""
		if !jctx.config.Vendor.RemoveNS {
			prefixPath = notif.Prefix.GetOrigin()
			if prefixPath != "" {
				prefixPath += gGnmiVerboseSensorDetailsDelim
			}
		}

		prefix := notif.GetPrefix()
		if prefix != nil {
			prefixPath = getPath(prefixPath, prefix.GetElem())
		}

		s += fmt.Sprintf(
			"Update {\n\ttimestamp: %d\n\tprefix: %v\n", notif.GetTimestamp(), prefixPath)

		// Parse all the deletes here
		for _, d := range notif.Delete {
			delPath := getPath(prefixPath, d.GetElem())
			s += fmt.Sprintf("del_path: %s", delPath)
		}

		// Parse all the updates here
		for _, u := range notif.Update {
			notifString := u.String()
			s += fmt.Sprintf("Update {\n\tpath {\n")
			re := regexp.MustCompile(`name:\"(.*?)\"`)
			matches := re.FindAllStringSubmatch(u.String(), -1)
			for _, match := range matches {
				s += fmt.Sprintf("\t\telem {\n\t\t\t")
				s += fmt.Sprintf("name: %s\n\t\t}\n", match[1])
			}

			// Define regular expression pattern to match "key:val"
			re = regexp.MustCompile(`val:\{(.*?)\}`)
			result := re.FindStringSubmatch(notifString)
			if len(result) > 1 {
				keyVal := strings.Split(result[1], ":")
				s += fmt.Sprintf("\t\tval {\n\t\t\t")
				s += fmt.Sprintf("%s: %s\n\t\t}\n", keyVal[0], keyVal[1])
			}

			s += fmt.Sprintf("\t}\n")
		}
		s += fmt.Sprintf("}\n")
	}
	jctx.config.InternalJtimon.logger.Printf(s)
}

func jLogInternalJtimonForPreGnmi(jctx *JCtx, ocdata *na_pb.OpenConfigData, outString string) {
	if jctx.config.InternalJtimon.logger == nil {
		return
	}

	// Log here in the format of internal jtimon
	jctx.config.InternalJtimon.preGnmiLogger.Printf("%s", outString)
}
