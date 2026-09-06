//go:build with_x264

package main

import (
	"github.com/thingify-app/thing-rtc/peer-go/codec"
	"github.com/thingify-app/thing-rtc/peer-go/codec/x264"
)

func makeCodec() (*codec.Codec, error) {
	return x264.NewCodec(500_000)
}
