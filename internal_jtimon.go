package main

import (
	"encoding/json"
	"fmt"
	gnmi "github.com/Juniper/jtimon/gnmi/gnmi"
	na_pb "github.com/Juniper/jtimon/telemetry"
	"log"
	"os"
	"regexp"
)

// InternalJtimonConfig type
type InternalJtimonConfig struct {
	DataLog       string `json:"data-log-file"`
	out           *os.File
	preGnmiOut    *os.File
	logger        *log.Logger
	preGnmiLogger *log.Logger
}

type InternalJtimonPathElem struct {
	Name string `json:"name"`
}

type InternalJtimonPath struct {
	Elems []InternalJtimonPathElem `json:"elem"`
}

type InternalJtimonVal struct {
	StringVal string `json:"string_val"`
}

type InternalJtimonUpdate struct {
	Path InternalJtimonPath `json:"path"`
	Val  InternalJtimonVal  `json:"val"`
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
}

func isInternalJtimonLogging(jctx *JCtx) bool {
	return jctx.config.InternalJtimon.logger != nil
}

func jLogInternalJtimonForGnmi(jctx *JCtx, parseOutput *gnmiParseOutputT, rsp *gnmi.SubscribeResponse) {
	if jctx.config.InternalJtimon.logger == nil {
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

	jctx.config.InternalJtimon.logger.Printf(s)

	notif := rsp.GetUpdate()
	if notif != nil {
		s += fmt.Sprintf("Update {\n\ttimestamp: %d\n\tprefix: %v\n", notif.GetTimestamp(), notif.Prefix)
		for _, u := range notif.Update {
			s += fmt.Sprintf("Update {\n\tpath {\n")
			s += fmt.Sprintf("%s\n", u.String())
			re := regexp.MustCompile(`name:\"(.*?)\"`)
			matches := re.FindAllStringSubmatch(u.String(), -1)
			jctx.config.InternalJtimon.logger.Println(matches)
			for _, match := range matches {
				s += fmt.Sprintf("\t\telem {\n\t\t\tname: %s\n\t\t\t}", match[1])
			}
			s += fmt.Sprintf("\t}\n")
			//jctx.config.InternalJtimon.logger.Printf(fmt.Sprintf("%s\n", u.String()))
			//jctx.config.InternalJtimon.logger.Printf(fmt.Sprintf("-----------------------"))
			//jctx.config.InternalJtimon.logger.Printf(fmt.Sprintf("%v\n", u.GetPath()))
			//jctx.config.InternalJtimon.logger.Printf(fmt.Sprintf("%v\n", u.GetVal()))
			//jctx.config.InternalJtimon.logger.Printf(fmt.Sprintf("-----------------------"))
		}
		s += fmt.Sprintf("}")
		jctx.config.InternalJtimon.logger.Printf(s)
		//jctx.config.InternalJtimon.logger.Printf(fmt.Sprintf("-----------------------"))
		//jctx.config.InternalJtimon.logger.Printf(fmt.Sprintf("%d\n", notif.Update))
		//jctx.config.InternalJtimon.logger.Printf(fmt.Sprintf("%v\n", rsp.GetUpdate()))
	}
}

func jLogInternalJtimonForPreGnmi(jctx *JCtx, ocdata *na_pb.OpenConfigData, outString string) {
	if jctx.config.InternalJtimon.logger == nil {
		return
	}

	// Log here in the format of internal jtimon
	jctx.config.InternalJtimon.preGnmiLogger.Printf("%s", outString)
}

func jLogUpdateOnChange(jctx *JCtx, kv map[string]string) {
	return
}
