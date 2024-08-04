//go:build !with_mmal
// +build !with_mmal

package main

import (
	"github.com/thingify-app/thing-rtc-go/codec"
	"github.com/thingify-app/thing-rtc-go/codec/openh264"
)

func makeCodec() (*codec.Codec, error) {
	return openh264.NewCodec(1_000_000)
}
