//go:build linux
// +build linux

package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"

	thingrtc "github.com/thingify-app/thing-rtc/peer-go"
	peerconfig "github.com/thingify-app/thing-rtc/peer-go/peer-config"
	"github.com/urfave/cli/v2"
	"github.com/vishvananda/netlink"
	"gvisor.dev/gvisor/pkg/tcpip/link/tun"
)

const SIGNALLING_SERVER_URL = "wss://thingify.deno.dev/signalling"
const DEFAULT_ADDRESS_RANGE = "10.0.1.1/24"
const REMOTE_HOST_IP = "10.0.1.2"

const MTU_BYTES = 16384

func setupTunInterface(name string) (int, error) {
	fd, err := tun.Open(name)

	if err != nil {
		return 0, err
	}

	link, err := netlink.LinkByName(name)
	if err != nil {
		return 0, err
	}

	addr, err := netlink.ParseAddr(DEFAULT_ADDRESS_RANGE)
	if err != nil {
		return 0, err
	}

	err = netlink.LinkSetMTU(link, MTU_BYTES)
	if err != nil {
		return 0, err
	}

	err = netlink.AddrAdd(link, addr)
	if err != nil {
		return 0, err
	}

	err = netlink.LinkSetUp(link)
	if err != nil {
		return 0, err
	}

	return fd, nil
}

func listenOnTun(peer thingrtc.Peer) error {
	tun, err := setupTunInterface("thingify0")
	if err != nil {
		return err
	}

	stack, err := createStack(REMOTE_HOST_IP, tun)
	if err != nil {
		return err
	}

	peer.OnDataChannel(func(dataChannel thingrtc.DataChannel) {
		err := handleNewDataChannel(stack, dataChannel)
		if err != nil {
			fmt.Printf("Failed to handle new data channel '%v': %v\n", dataChannel.GetLabel(), err)
		}
	})

	// Block forever waiting for data channels:
	select {}
}

func handleNewDataChannel(stack *NetworkStack, dataChannel thingrtc.DataChannel) error {
	label := dataChannel.GetLabel()
	protocol, targetIp, targetPort, err := parseLabel(label)
	if err != nil {
		return err
	}

	dcStream, err := dataChannel.AsStream()
	if err != nil {
		return err
	}

	var conn net.Conn

	if protocol == "tcp" {
		conn, err = stack.DialTCP(targetIp, targetPort)
	} else if protocol == "udp" {
		conn, err = stack.DialUDP(targetIp, targetPort)
	}

	if err != nil {
		dcStream.Close()
		return err
	}

	interceptor := &Interceptor{Stream: dcStream}

	bridgeStreams(interceptor, conn)
	return nil
}

func parseLabel(label string) (string, string, uint16, error) {
	parts := strings.Split(label, ":")
	if len(parts) != 3 {
		return "", "", 0, fmt.Errorf("Expected label to be 3 colon-separated parts, was: %v", parts)
	}

	protocol, targetIp, portStr := parts[0], parts[1], parts[2]

	if protocol != "tcp" && protocol != "udp" {
		return "", "", 0, fmt.Errorf("Invalid protocol (expected tcp or udp): %v", protocol)
	}

	targetPort, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return "", "", 0, fmt.Errorf("Invalid port: %v", portStr)
	}

	return protocol, targetIp, uint16(targetPort), nil
}

func bridgeStreams(webrtcConn, netConn io.ReadWriteCloser) {
	// WebRTC -> Network stack
	go func() {
		defer netConn.Close()
		defer webrtcConn.Close()
		_, err := io.Copy(netConn, webrtcConn)
		fmt.Println("Copy webrtc to network ended")
		if err != nil {
			fmt.Printf("Connection failed: %v\n", err)
		}
	}()

	// Network stack -> WebRTC
	go func() {
		defer netConn.Close()
		defer webrtcConn.Close()
		_, err := io.Copy(webrtcConn, netConn)
		fmt.Println("Copy network to webrtc ended")
		if err != nil {
			fmt.Printf("Connection failed: %v\n", err)
		}
	}()
}

// Interceptor wraps an existing io.ReadWriteCloser
type Interceptor struct {
	Stream io.ReadWriteCloser
}

// Read intercepts incoming data
func (i *Interceptor) Read(p []byte) (n int, err error) {
	n, err = i.Stream.Read(p)
	if n > 0 {
		log.Printf("Intercepted Read (len %v): %x\n", n, p[:n]) // Or string(p[:n]) for text
	}
	return n, err
}

// Write intercepts outgoing data
func (i *Interceptor) Write(p []byte) (n int, err error) {
	log.Printf("Intercepted Write (len %v): %x\n", len(p), p)
	return i.Stream.Write(p)
}

// Close delegates the close operation
func (i *Interceptor) Close() error {
	log.Println("Closing stream...")
	return i.Stream.Close()
}

func createPeer(sharedSecretBase64 string, withMedia bool, useRtsp bool, rtspUrl string) (peer thingrtc.Peer, err error) {
	peerConfig, err := peerconfig.CreateInitiatorConfigWithSecret(sharedSecretBase64)
	if err != nil {
		return nil, err
	}
	serverAuth := thingrtc.CreateInsecureServerAuth(peerConfig.PairingId, peerConfig.Role)

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
		return thingrtc.NewPeerWithMedia(SIGNALLING_SERVER_URL, serverAuth, peerConfig, true, videoSource), nil
	} else {
		return thingrtc.NewPeer(SIGNALLING_SERVER_URL, serverAuth, peerConfig, true), nil
	}
}

func connect(sharedSecretBase64 string, withMedia bool, withRtsp bool, rtspUrl string) error {
	peer, err := createPeer(sharedSecretBase64, withMedia, withRtsp, rtspUrl)
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
				Name:  "connect",
				Usage: "Create an network interface to a peer",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "secret",
						Usage:    "shared secret of the peer to connect to",
						Required: true,
					},
					&cli.BoolFlag{
						Name:  "withMedia",
						Usage: "Set to enable media (camera) streaming",
						Value: false,
					},
				},
				Action: func(ctx *cli.Context) error {
					return connect(ctx.String("secret"), ctx.Bool("withMedia"), withRtsp, rtspUrl)
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		panic(err)
	}
}
