package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/encoding/gzip"
)

func getSecurityOptions(jctx *JCtx) (grpc.DialOption, error) {
	var bs []byte
	var err error

	if jctx.config.TLS.CA == "" {
		return grpc.WithInsecure(), nil
	}

	certificate, _ := tls.LoadX509KeyPair(jctx.config.TLS.ClientCrt, jctx.config.TLS.ClientKey)
	certPool := x509.NewCertPool()
	if bs, err = ioutil.ReadFile(jctx.config.TLS.CA); err != nil {
		return nil, fmt.Errorf("[%s] failed to read ca cert: %s", jctx.config.Host, err)
	}

	if ok := certPool.AppendCertsFromPEM(bs); !ok {
		return nil, fmt.Errorf("[%s] failed to append certs", jctx.config.Host)
	}

	transportCreds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{certificate},
		ServerName:   jctx.config.TLS.ServerName,
		RootCAs:      certPool,
	})

	return grpc.WithTransportCredentials(transportCreds), nil
}

func getGPRCDialOptions(jctx *JCtx, vendor *vendor) ([]grpc.DialOption, error) {
	var opts []grpc.DialOption

	if securityOpt, err := getSecurityOptions(jctx); err == nil {
		opts = append(opts, securityOpt)
	} else {
		return nil, err
	}

	if *statsHandler {
		opts = append(opts, grpc.WithStatsHandler(&statshandler{jctx: jctx}))
		if isCsvStatsEnabled(jctx) {
			jctx.config.InternalJtimon.csvLogger.Printf(fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
				"sensor-path", "sequence-number", "component-id", "sub-component-id", "packet-size", "p-ts", "e-ts", "re-stream-creation-ts", "re-payload-get-ts"))
		}
	}

	switch *compression {
	case "gzip":
		compressionOpts := grpc.WithDefaultCallOptions(grpc.UseCompressor(gzip.Name))
		opts = append(opts, compressionOpts)
		jLog(jctx, "compression = gzip")
	default:
		jLog(jctx, "compression = none")
	}

	ws := jctx.config.GRPC.WS
	opts = append(opts, grpc.WithInitialWindowSize(ws))
	opts = append(opts, grpc.WithInitialConnWindowSize(ws))

	if vendor.dialExt != nil {
		opt := vendor.dialExt(jctx)
		if opt != nil {
			opts = append(opts, opt)
		}
	}
	return opts, nil
}
