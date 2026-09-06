//go:build linux

package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	thingrtc "github.com/thingify-app/thing-rtc/peer-go"
	peerconfig "github.com/thingify-app/thing-rtc/peer-go/peer-config"
	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v3"
)

const SIGNALLING_SERVER_URL = "wss://signalling.thingify.app/signalling"
const INTERFACE_NAME = "thingify0"
const DEFAULT_ADDRESS_RANGE = "10.0.1.1/24"
const START_HOST_IP = "10.0.1.2"

func handleNewPeer(stack *NetworkStack, peer thingrtc.Peer) error {
	// We do not need to release the client, because there is a fixed number of
	// peers and each persists forever.
	client, err := stack.LeaseClient()
	if err != nil {
		return err
	}

	peer.OnDataChannel(func(dataChannel thingrtc.DataChannel) {
		err := handleNewDataChannel(client, dataChannel)
		if err != nil {
			fmt.Printf("Failed to handle new data channel '%v': %v\n", dataChannel.GetLabel(), err)
		}
	})

	return nil
}

func handleNewDataChannel(client *Client, dataChannel thingrtc.DataChannel) error {
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
		conn, err = client.DialTCP(targetIp, targetPort)
	} else if protocol == "udp" {
		conn, err = client.DialUDP(targetIp, targetPort)
	}

	if err != nil {
		dcStream.Close()
		return err
	}

	bridgeStreams(dcStream, conn)
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

func connect(config *Config) error {
	stack, err := CreateStack(INTERFACE_NAME, START_HOST_IP)
	if err != nil {
		return err
	}

	// Create one peer for each sharedSecret:
	for _, sharedSecret := range config.SharedSecrets {
		peer, err := createPeer(sharedSecret, config.WithMedia, config.WithRtsp, config.RtspUrl)
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

		err = handleNewPeer(stack, peer)
		if err != nil {
			return err
		}
	}

	// Block forever for peers to connect/re-connect:
	select {}
}

type Config struct {
	SharedSecrets []string `yaml:"shared_secrets"`
	WithMedia     bool     `yaml:"with_media"`
	WithRtsp      bool     `yaml:"with_rtsp"`
	RtspUrl       string   `yaml:"rtsp_url"`
}

func loadConfig(configFile string) (*Config, error) {
	yamlFile, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var config Config

	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func main() {
	app := cli.App{
		Name:  "thingify-net",
		Usage: "Create virtual networks with web browsers over WebRTC.",
		Commands: []*cli.Command{
			{
				Name:  "connect",
				Usage: "Create a network interface to peers",
				Flags: []cli.Flag{
					&cli.PathFlag{
						Name:     "config",
						Usage:    "path to the YAML config file",
						Required: true,
					},
				},
				Action: func(ctx *cli.Context) error {
					config, err := loadConfig(ctx.Path("config"))
					if err != nil {
						return err
					}
					return connect(config)
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		panic(err)
	}
}
