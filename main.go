//go:build linux

package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	thingrtc "github.com/thingify-app/thing-rtc/peer-go"
	"github.com/thingify-app/thing-rtc/peer-go/pairing"
	peerconfig "github.com/thingify-app/thing-rtc/peer-go/peer-config"
	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v3"
)

const SIGNALLING_SERVER_URL = "wss://signalling.thingify.app/signalling"

const DEFAULT_CONFIG_DIR = "/etc/thingify"
const DEFAULT_CONFIG_FILE = DEFAULT_CONFIG_DIR + "/thingify_config.yaml"
const DEFAULT_PRIVATE_KEY_FILE = DEFAULT_CONFIG_DIR + "/private_key.json"
const DEFAULT_TRUSTED_KEY_FILE = DEFAULT_CONFIG_DIR + "/trusted_keys.yaml"

const INTERFACE_NAME = "thingify0"
const DEFAULT_ADDRESS_RANGE = "10.0.1.1/24"
const START_HOST_IP = "10.0.1.2"
const MAX_CONNS_PER_PEER = 2

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

func createPeer(peerConfig *peerconfig.PeerConfig, mediaSource *thingrtc.MediaSource) thingrtc.Peer {
	serverAuth := thingrtc.CreateInsecureServerAuth(peerConfig.PairingId, peerConfig.Role)

	if mediaSource != nil {
		return thingrtc.NewPeerWithMedia(SIGNALLING_SERVER_URL, serverAuth, peerConfig, true, mediaSource)
	} else {
		return thingrtc.NewPeer(SIGNALLING_SERVER_URL, serverAuth, peerConfig, true)
	}
}

func createMediaSource(withMedia bool, withRtsp bool, rtspUrl string) (*thingrtc.MediaSource, error) {
	if withMedia {
		if withRtsp {
			return thingrtc.CreateRtspMediaSource(rtspUrl)
		} else {
			codec, err := makeCodec()
			if err != nil {
				return nil, err
			}
			return thingrtc.CreateVideoMediaSource(codec, 640, 480)
		}
	}
	return nil, nil
}

func createPeersForConfig(stack *NetworkStack, peerConfig *peerconfig.PeerConfig, mediaSource *thingrtc.MediaSource) error {
	// Create a peer for each potential connection from this public key.
	for i := 0; i < MAX_CONNS_PER_PEER; i++ {
		peer := createPeer(peerConfig, mediaSource)

		err := handleNewPeer(stack, peer)
		if err != nil {
			return err
		}
	}
	return nil
}

func createKeyPairPeers(stack *NetworkStack, config *Config, mediaSource *thingrtc.MediaSource) error {
	trustedKeys, err := loadTrustedKeys(config.TrustedKeysFile)
	if err != nil {
		return err
	}

	// Create peers for each public key:
	for _, publicKey := range trustedKeys {
		localKeyPair, err := loadLocalKeyPair(config.PrivateKeyFile)
		if err != nil {
			return err
		}

		peerConfig, err := peerconfig.CreateKeyPairConfig(publicKey, *localKeyPair, "initiator")
		if err != nil {
			return err
		}

		err = createPeersForConfig(stack, peerConfig, mediaSource)
		if err != nil {
			return err
		}
	}

	return nil
}

func createSharedSecretPeers(stack *NetworkStack, config *Config, mediaSource *thingrtc.MediaSource) error {
	// Create peers for each shared secret:
	for _, sharedSecret := range config.SharedSecrets {
		peerConfig, err := peerconfig.CreateInitiatorConfigWithSecret(sharedSecret)
		if err != nil {
			return err
		}

		err = createPeersForConfig(stack, peerConfig, mediaSource)
		if err != nil {
			return err
		}
	}
	return nil
}

func connect(config *Config) error {
	stack, err := CreateStack(INTERFACE_NAME, START_HOST_IP)
	if err != nil {
		return err
	}

	// Create a single MediaSource, which is then reused by all peer connections
	// to avoid subscribing to a potential single source multiple times.
	mediaSource, err := createMediaSource(config.WithMedia, config.WithRtsp, config.RtspUrl)
	if err != nil {
		return err
	}

	err = createSharedSecretPeers(stack, config, mediaSource)
	if err != nil {
		return err
	}

	err = createKeyPairPeers(stack, config, mediaSource)
	if err != nil {
		return err
	}

	// Block forever for peers to connect/re-connect:
	select {}
}

func loadLocalKeyPair(privateKeyFile string) (*pairing.KeyPair, error) {
	data, err := os.ReadFile(privateKeyFile)
	if err != nil {
		return nil, err
	}

	return peerconfig.LoadLocalKeyPair(string(data))
}

func generateLocalKeyPair(privateKeyFile string) error {
	keyPair, err := peerconfig.CreateLocalKeyPair()
	if err != nil {
		return err
	}

	jwk := keyPair.PrivateKey.ExportJwk()

	// Only allow the current user to read/write the file:
	err = os.WriteFile(privateKeyFile, []byte(jwk), 0600)
	if err != nil {
		return err
	}

	publicKeySpki := base64.StdEncoding.EncodeToString(keyPair.PublicKey.ExportSpki())
	fmt.Printf("Generated keypair with public key: %v\n", publicKeySpki)

	return nil
}

func printLocalPublicKey(privateKeyFile string) error {
	keyPair, err := loadLocalKeyPair(privateKeyFile)
	if err != nil {
		return err
	}

	spki := keyPair.PublicKey.ExportSpki()
	fmt.Println(base64.StdEncoding.EncodeToString(spki))

	return nil
}

func listTrustedKeys(trustedKeyFile string) error {
	keys, err := loadTrustedKeys(trustedKeyFile)
	if err != nil {
		return err
	}

	for _, k := range keys {
		fmt.Printf("- %v\n", base64.StdEncoding.EncodeToString(k.ExportSpki()))
	}

	return nil
}

func addTrustedKey(publicKey string, trustedKeyFile string, privateKeyFile string) error {
	keys, err := loadTrustedKeys(trustedKeyFile)
	if err != nil {
		return err
	}

	parsedKey, err := peerconfig.CreateRemoteKey(publicKey)
	if err != nil {
		return err
	}

	keys = append(keys, parsedKey)
	err = saveTrustedKeys(trustedKeyFile, keys)
	if err != nil {
		return err
	}

	fmt.Println("Paste this public key into the pairing window:")
	printLocalPublicKey(privateKeyFile)
	return nil
}

type TrustedKeys struct {
	TrustedKeys []string `yaml:"trusted_keys"`
}

func loadTrustedKeys(file string) ([]pairing.PublicKey, error) {
	yamlFile, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var trustedKeys TrustedKeys

	err = yaml.Unmarshal(yamlFile, &trustedKeys)
	if err != nil {
		return nil, err
	}

	publicKeys := make([]pairing.PublicKey, len(trustedKeys.TrustedKeys))
	for i, key := range trustedKeys.TrustedKeys {
		publicKey, err := peerconfig.CreateRemoteKey(key)
		if err != nil {
			return nil, err
		}
		publicKeys[i] = publicKey
	}

	return publicKeys, nil
}

func saveTrustedKeys(file string, keys []pairing.PublicKey) error {
	spkiKeys := make([]string, len(keys))
	for i, key := range keys {
		spkiKeys[i] = base64.StdEncoding.EncodeToString(key.ExportSpki())
	}

	trustedKeys := TrustedKeys{
		TrustedKeys: spkiKeys,
	}
	data, err := yaml.Marshal(trustedKeys)
	if err != nil {
		return err
	}

	err = os.WriteFile(file, data, 0644)
	if err != nil {
		return err
	}

	return nil
}

type Config struct {
	WithMedia       bool     `yaml:"with_media"`
	WithRtsp        bool     `yaml:"with_rtsp"`
	RtspUrl         string   `yaml:"rtsp_url"`
	SharedSecrets   []string `yaml:"shared_secrets"`
	PrivateKeyFile  string   `yaml:"private_key_file"`
	TrustedKeysFile string   `yaml:"trusted_keys_file"`
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
						Name:  "config",
						Usage: "path to the YAML config file",
						Value: DEFAULT_CONFIG_FILE,
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
			{
				Name:  "generateKeyPair",
				Usage: "Generates a new keypair and saves it in the given file",

				Flags: []cli.Flag{
					&cli.PathFlag{
						Name:  "file",
						Usage: "path of the private key file to create",
						Value: DEFAULT_PRIVATE_KEY_FILE,
					},
				},
				Action: func(ctx *cli.Context) error {
					return generateLocalKeyPair(ctx.Path("file"))
				},
			},
			{
				Name:  "printPublicKey",
				Usage: "Prints the public key from the given private key file",

				Flags: []cli.Flag{
					&cli.PathFlag{
						Name:  "file",
						Usage: "path of the private key file to read",
						Value: DEFAULT_PRIVATE_KEY_FILE,
					},
				},
				Action: func(ctx *cli.Context) error {
					return printLocalPublicKey(ctx.Path("file"))
				},
			},
			{
				Name:  "listTrustedKeys",
				Usage: "Lists the saved trusted public keys",

				Flags: []cli.Flag{
					&cli.PathFlag{
						Name:  "file",
						Usage: "path of the trusted key file to read",
						Value: DEFAULT_TRUSTED_KEY_FILE,
					},
				},
				Action: func(ctx *cli.Context) error {
					return listTrustedKeys(ctx.Path("file"))
				},
			},
			{
				Name:  "addTrustedKey",
				Usage: "Adds the provided public key to the trusted keys and prints its own",

				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "publicKey",
						Usage:    "base64-encoded public key to add to trusted keys",
						Required: true,
					},
					&cli.PathFlag{
						Name:  "trustedKeyFile",
						Usage: "path of the trusted key file to read",
						Value: DEFAULT_TRUSTED_KEY_FILE,
					},
					&cli.PathFlag{
						Name:  "privateKeyFile",
						Usage: "path of the private key file to read",
						Value: DEFAULT_PRIVATE_KEY_FILE,
					},
				},
				Action: func(ctx *cli.Context) error {
					return addTrustedKey(ctx.String("publicKey"), ctx.Path("trustedKeyFile"), ctx.Path("privateKeyFile"))
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		panic(err)
	}
}
