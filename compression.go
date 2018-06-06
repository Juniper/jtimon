/*
 * Copyright (c) 2018, Juniper Networks, Inc.
 * All rights reserved.
 */

package main

import (
	"compress/zlib"
	"io"
	"io/ioutil"

	"google.golang.org/grpc"
)

func newDEFLATEDecompressor() grpc.Decompressor {
	return &deflateDecompressor{}
}

type deflateDecompressor struct {
}

func (d *deflateDecompressor) Do(r io.Reader) ([]byte, error) {
	z, err := zlib.NewReader(r)
	if err != nil {
		return nil, err
	}

	a, err := ioutil.ReadAll(z)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (d *deflateDecompressor) Type() string {
	return "deflate"
}

// End Deflate Decompression
