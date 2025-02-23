package main

import (
	"fmt"
	"io"
	"os"
	"path"

	"github.com/songgao/water"
	thingrtc "github.com/thingify-app/thing-rtc-go"
	"github.com/thingify-app/thing-rtc-go/pairing"
	"github.com/urfave/cli/v2"
	"github.com/vishvananda/netlink"
)

const PAIRING_SERVER_URL = "https://thingify.deno.dev/pairing"
const SIGNALLING_SERVER_URL = "wss://thingify.deno.dev/signalling"
const DEFAULT_ADDRESS_RANGE = "10.0.1.0/24"

// Not sure why MTU needs to be this, but higher values (e.g. 1200) result in
// data channel messages not being sent - perhaps an SCTP limitation?
const MTU_BYTES = 1024

func setupTunInterface(name string) (io.ReadWriteCloser, error) {
	tun, err := water.New(water.Config{
		DeviceType: water.TUN,
		PlatformSpecificParams: water.PlatformSpecificParams{
			Name: name,
		},
	})

	if err != nil {
		return nil, err
	}

	link, err := netlink.LinkByName(tun.Name())
	if err != nil {
		return nil, err
	}

	addr, err := netlink.ParseAddr(DEFAULT_ADDRESS_RANGE)
	if err != nil {
		return nil, err
	}

	err = netlink.LinkSetMTU(link, MTU_BYTES)
	if err != nil {
		return nil, err
	}

	err = netlink.AddrAdd(link, addr)
	if err != nil {
		return nil, err
	}

	err = netlink.LinkSetUp(link)
	if err != nil {
		return nil, err
	}

	return tun, nil
}

func createPairing() pairing.Pairing {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		panic(err)
	}

	// Create our config dir if it doesn't exist.
	configDir := path.Join(userConfigDir, "thingrtc")
	err = os.MkdirAll(configDir, os.ModePerm)
	if err != nil {
		panic(err)
	}

	return pairing.NewPairing(PAIRING_SERVER_URL, path.Join(configDir, "pairing.json"))
}

func respondToPairing(shortcode string) error {
	pairing := createPairing()
	pairing.ClearAllPairings()

	fmt.Printf("Responding to pairing...\n")
	result, err := pairing.RespondToPairing(shortcode)
	if err != nil {
		return err
	}

	fmt.Printf("Pairing succeeded, pairingId: %v\n", result.PairingId)
	return nil
}

func listenOnTun(peer thingrtc.Peer) error {
	tun, err := setupTunInterface("thingify0")
	if err != nil {
		return err
	}

	peer.OnBinaryMessage(func(message []byte) {
		tun.Write(message)
	})

	buffer := make([]byte, MTU_BYTES)
	for {
		n, err := tun.Read(buffer)
		if err != nil {
			return err
		}

		peer.SendBinaryMessage(buffer[:n])
	}
}

func createPeer(tokenGenerator pairing.TokenGenerator, withMedia bool, useRtsp bool, rtspUrl string) (peer thingrtc.Peer, err error) {
	if withMedia {
		var videoSource *thingrtc.MediaSource
		if useRtsp {
			videoSource, err = thingrtc.CreateRtspMediaSource(rtspUrl)
			if err != nil {
				return nil, err
			}
		} else {
			codec, err := makeCodec()
			if err != nil {
				return nil, err
			}
			videoSource, err = thingrtc.CreateVideoMediaSource(codec, 640, 480)
			if err != nil {
				return nil, err
			}
		}
		return thingrtc.NewPeerWithMedia(SIGNALLING_SERVER_URL, tokenGenerator, videoSource), nil
	} else {
		return thingrtc.NewPeer(SIGNALLING_SERVER_URL, tokenGenerator), nil
	}
}

func connect(withMedia bool, withRtsp bool, rtspUrl string) error {
	pairing := createPairing()
	pairingIds := pairing.GetAllPairingIds()
	if len(pairingIds) == 0 {
		return fmt.Errorf("pairing not setup, re-run pairing")
	}
	pairingId := pairingIds[0]

	tokenGenerator, err := pairing.GetTokenGenerator(pairingId)
	if err != nil {
		return err
	}

	peer, err := createPeer(tokenGenerator, withMedia, withRtsp, rtspUrl)
	if err != nil {
		return err
	}

	peer.OnConnectionStateChange(func(connectionState int) {
		switch connectionState {
		case thingrtc.Disconnected:
			fmt.Println("Disconnected")
		case thingrtc.Connecting:
			fmt.Println("Connecting...")
		case thingrtc.Connected:
			fmt.Println("Connected.")
		}
	})

	peer.Connect()

	err = listenOnTun(peer)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	withRtsp := (os.Getenv("USE_RTSP") == "true")
	rtspUrl := os.Getenv("RTSP_URL")
	app := cli.App{
		Name:  "thingify-net",
		Usage: "Create virtual networks with web browsers over WebRTC.",
		Commands: []*cli.Command{
			{
				Name:  "pair",
				Usage: "Manage pairing with a peer",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "shortcode",
						Usage:    "shortcode provided by initiating peer",
						Required: true,
					},
				},
				Action: func(ctx *cli.Context) error {
					return respondToPairing(ctx.String("shortcode"))
				},
			},
			{
				Name:  "connect",
				Usage: "Create an network interface to a peer",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "withMedia",
						Usage: "Set to enable media (camera) streaming",
						Value: false,
					},
				},
				Action: func(ctx *cli.Context) error {
					return connect(ctx.Bool("withMedia"), withRtsp, rtspUrl)
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		panic(err)
	}
}
