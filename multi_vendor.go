package main

import (
	"fmt"
	"google.golang.org/grpc"
)

var vendors = []*vendor{newGNMI(), newJuniperJUNOS(), newCiscoIOSXR(), newPrePostGNMI()}

type vendor struct {
	name               string
	loginCheckRequired bool
	sendLoginCheck     func(*JCtx, *grpc.ClientConn) error
	dialExt            func(*JCtx) grpc.DialOption
	subscribe          func(conn *grpc.ClientConn, jctx *JCtx, cfg Config) SubErrorCode
}

func getVendor(jctx *JCtx, tryGnmi bool, tryPrePostGnmi bool) (*vendor, error) {
	name := jctx.config.Vendor.Name

	if tryGnmi {
		name = "gnmi"
	} else if tryPrePostGnmi {
		name = "pre-post-gnmi"
	}
	// juniper-junos is default
	if name == "" {
		name = "juniper-junos"
	}
	for _, vendor := range vendors {
		if name == vendor.name {
			return vendor, nil
		}
	}
	return nil, fmt.Errorf("support for vendor [%s] has not implemented yet", name)
}

func newJuniperJUNOS() *vendor {
	return &vendor{
		name:               "juniper-junos",
		loginCheckRequired: true,
		sendLoginCheck:     loginCheckJunos,
		dialExt:            nil,
		subscribe:          subscribeJunos,
	}
}

func newCiscoIOSXR() *vendor {
	return &vendor{
		name:               "cisco-iosxr",
		loginCheckRequired: false,
		sendLoginCheck:     nil,
		dialExt:            dialExtensionXR,
		subscribe:          subscribeXR,
	}
}

func newGNMI() *vendor {
	return &vendor{
		name:               "gnmi",
		loginCheckRequired: false,
		sendLoginCheck:     nil,
		dialExt:            nil,
		subscribe:          subscribegNMI,
	}
}

func newPrePostGNMI() *vendor {
	return &vendor{
		name:               "pre-post-gnmi",
		loginCheckRequired: false,
		sendLoginCheck:     nil,
		dialExt:            nil,
		subscribe:          subscribePrePostGNMI,
	}
}

func subscribePrePostGNMI(conn *grpc.ClientConn, jctx *JCtx, cfg Config) SubErrorCode {
	// Create channels for receiving results
	gnmiResultCh := make(chan SubErrorCode)
	junosResultCh := make(chan SubErrorCode)

	// Launch goroutines for each subscription function
	go func() {
		cfg.Paths = getGnmiPaths(cfg)
		gnmiResultCh <- subscribegNMI(conn, jctx, cfg)
	}()

	go func() {
		cfg.Paths = getPreGnmiPaths(cfg)
		junosResultCh <- subscribeJunos(conn, jctx, cfg)
	}()

	// Use select to wait for the first result to be available
	select {
	case result := <-gnmiResultCh:
		// Process result from subscribeGNMI
		return result
	case result := <-junosResultCh:
		// Process result from subscribeJunos
		return result
	}
}

func getGnmiPaths(config Config) []PathsConfig {
	var paths []PathsConfig
	for _, p := range config.Paths {
		if p.GNMI || (config.Vendor.Gnmi != nil && !p.PreGnmi) {
			paths = append(paths, p)
		}
	}
	return paths
}

func getPreGnmiPaths(config Config) []PathsConfig {
	var paths []PathsConfig
	for _, p := range config.Paths {
		if p.PreGnmi || (config.Vendor.Gnmi == nil && !p.GNMI) {
			paths = append(paths, p)
		}
	}
	return paths
}
