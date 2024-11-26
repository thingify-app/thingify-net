//go:build !with_mmal
// +build !with_mmal

package main

import (
	"github.com/thingify-app/thing-rtc-go/codec"
	"github.com/thingify-app/thing-rtc-go/codec/x264"
)

func makeCodec() (*codec.Codec, error) {
	return x264.NewCodec(500_000)
}
