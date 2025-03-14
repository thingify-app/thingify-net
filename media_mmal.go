//go:build with_mmal
// +build with_mmal

package main

import (
	"github.com/thingify-app/thing-rtc/peer-go/codec"
	"github.com/thingify-app/thing-rtc/peer-go/codec/mmal"
)

func makeCodec() (*codec.Codec, error) {
	return mmal.NewCodec(1_000_000)
}
