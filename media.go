//go:build !with_x264 && !with_mmal

package main

import (
	"fmt"

	"github.com/thingify-app/thing-rtc/peer-go/codec"
)

func makeCodec() (*codec.Codec, error) {
	return nil, fmt.Errorf("Built with no codecs")
}
