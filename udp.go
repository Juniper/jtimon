/*
 * Copyright (c) 2018, Juniper Networks, Inc.
 * All rights reserved.
 */

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"

	"github.com/Juniper/jtimon/protos/qmon"
	tt "github.com/Juniper/jtimon/protos/telemetry_top"
	"github.com/golang/protobuf/proto"
)

func handleUDPConnection(conn *net.UDPConn) {

	buffer := make([]byte, 4096)

	n, _, err := conn.ReadFromUDP(buffer)

	ts := &tt.TelemetryStream{}
	if err := proto.Unmarshal(buffer[:n], ts); err != nil {
		log.Fatalln("Failed to parse: ", err)
	}

	if *outJSON {
		b, err := json.MarshalIndent(ts, "", "  ")
		if err != nil {
			log.Fatalln("JSON indent error: ", err)
		}
		fmt.Printf("%s\n", b)
	}
	if proto.HasExtension(ts.Enterprise, tt.E_JuniperNetworks) {
		jnsi, err := proto.GetExtension(ts.Enterprise, tt.E_JuniperNetworks)
		if err != nil {
			log.Fatalln("Cant get extension: ", err)
		}
		switch jns := jnsi.(type) {
		case *tt.JuniperNetworksSensors:
			if proto.HasExtension(jns, qmon.E_JnprQmonExt) {
				qmi, err := proto.GetExtension(jns, qmon.E_JnprQmonExt)
				if err != nil {
					log.Fatalln("Cant get extension: ", err)
				}
				switch qm := qmi.(type) {
				case *qmon.QueueMonitor:
					if *outJSON {
						b, err := json.MarshalIndent(qm, "", "  ")
						if err != nil {
							log.Fatalln("JSON indent error: ", err)
						}
						fmt.Printf("%s\n", b)
					}
				}
			}
		}
	}

	if err != nil {
		log.Fatal(err)
	}
}

func udpInit() {
	address := fmt.Sprintf("%s:%d", "0.0.0.0", *port)
	udpAddr, err := net.ResolveUDPAddr("udp4", address)

	if err != nil {
		log.Fatal(err)
	}

	ln, err := net.ListenUDP("udp", udpAddr)

	if err != nil {
		log.Fatal(err)
	}

	defer ln.Close()

	for {
		handleUDPConnection(ln)
	}
}
