package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	gnmi "github.com/Juniper/jtimon/gnmi/gnmi"
	na_pb "github.com/Juniper/jtimon/telemetry"
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
	if jctx.config.InternalJtimon.DataLog != "" {
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
	}

	if *statsHandler && jctx.config.InternalJtimon.CsvLog != "" {
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
	if *statsHandler && jctx.config.InternalJtimon.CsvLog != "" {
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
	if jctx.config.InternalJtimon.logger == nil || parseOutput.jHeader == nil || jctx.config.InternalJtimon.DataLog == "" {
		return
	}

	s := "" // Keep the original string format
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

	// Prepare outputData for JSON format
	outputData := make(map[string]interface{}) // Map to hold the JSON output

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
			// Add to plain string format
			s += fmt.Sprintf("%s: %s\n", v, strVal)

			// Add to JSON structure
			outputData[v] = strVal
		}
	}

	notif := rsp.GetUpdate()
	if notif != nil {
		// Plain text formatting
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

		// Create a map to hold notification data for JSON output
		notifData := make(map[string]interface{})
		notifData["timestamp"] = notif.GetTimestamp()
		notifData["prefix"] = prefixPath

		// Parse delete paths (both for string and JSON)
		var delPaths []string
		for _, d := range notif.Delete {
			delPath := getPath(prefixPath, d.GetElem())
			s += fmt.Sprintf("del_path: %s\n", delPath)
			delPaths = append(delPaths, delPath)
		}
		notifData["del_paths"] = delPaths

		// Parse updates (both for string and JSON)
		var updates []map[string]interface{}
		for _, u := range notif.Update {
			s += fmt.Sprintf("Update {\n\tpath {\n")

			update := make(map[string]interface{})

			re := regexp.MustCompile(`name:\"(.*?)\"`)
			matches := re.FindAllStringSubmatch(u.String(), -1)
			var elems []string
			for _, match := range matches {
				s += fmt.Sprintf("\t\telem {\n\t\t\tname: %s\n\t\t}\n", match[1])
				elems = append(elems, match[1])
			}
			update["elems"] = elems

			// Extract key-value pairs for the update
			notifString := u.String()
			re = regexp.MustCompile(`val:\{(.*)\}`)
			result := re.FindStringSubmatch(notifString)
			if len(result) > 1 {
				keyVal := strings.SplitN(result[1], ":", 2)
				s += fmt.Sprintf("\t\tval {\n\t\t\t%s: %s\n\t\t}\n", keyVal[0], keyVal[1])
				update["key"] = keyVal[0]
				update["value"] = strings.Trim(keyVal[1], "\"")
			}

			updates = append(updates, update)
			s += fmt.Sprintf("\t}\n")
		}
		s += fmt.Sprintf("}\n")
		notifData["updates"] = updates // Add update data to JSON output

		outputData["notification"] = notifData // Add notification to JSON output
	}

	if *outJSON {
		// Marshal the JSON data and print it
		jsonOutput, err := json.MarshalIndent(outputData, "", "  ")
		if err != nil {
			jLog(jctx, "Error marshaling output to JSON")
			return
		}
		jctx.config.InternalJtimon.logger.Printf("%s\n", jsonOutput)
	} else {
		// Print the plain text output
		jctx.config.InternalJtimon.logger.Print(s)
	}

}

func jLogInternalJtimonForPreGnmi(jctx *JCtx, ocdata *na_pb.OpenConfigData, outString string) {
	if jctx.config.InternalJtimon.logger == nil || jctx.config.InternalJtimon.DataLog == "" {
		return
	}

	// Log here in the format of internal jtimon
	jctx.config.InternalJtimon.preGnmiLogger.Printf("%s", outString)
}
