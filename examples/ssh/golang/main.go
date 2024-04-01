//go:build js && wasm

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net"
	"syscall/js"
	"time"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/link/sniffer"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
)

func createStack(localAddrName string) (*stack.Stack, error) {
	rand.Seed(time.Now().UnixNano())

	localAddr := tcpip.AddrFromSlice(net.ParseIP(localAddrName).To4())

	// Create the stack with ipv4 and tcp protocols, then add a tun-based
	// NIC and ipv4 address.
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol},
	})

	linkEP := channel.New(0, 1200, "")
	if err := s.CreateNIC(1, sniffer.New(linkEP)); err != nil {
		return nil, fmt.Errorf("error creating NIC: %v", err)
	}

	incomingBuf := make([]byte, 2048)
	incomingMessageBuffer := js.Global().Get("outgoingMessageBuffer")
	messageListener := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		go func() {
			len := args[0].Int()
			js.CopyBytesToGo(incomingBuf, incomingMessageBuffer)
			fmt.Println("binary message received: " + hex.EncodeToString(incomingBuf[:len]))
			linkEP.InjectInbound(ipv4.ProtocolNumber, stack.NewPacketBuffer(stack.PacketBufferOptions{
				Payload: buffer.MakeWithData(incomingBuf[:len]),
			}))
		}()
		return nil
	})

	js.Global().Set("messageListener", messageListener)
	messageBuffer := js.Global().Get("messageBuffer")

	go func() {
		for {
			packetBuffer := linkEP.ReadContext(context.Background())
			buffer := packetBuffer.ToBuffer()
			flatBuffer := buffer.Flatten()
			fmt.Println("Sending to JS: " + hex.EncodeToString(flatBuffer))
			js.CopyBytesToJS(messageBuffer, flatBuffer)
			js.Global().Call("sendToPeer", len(flatBuffer))
		}
	}()

	protocolAddr := tcpip.ProtocolAddress{
		Protocol:          ipv4.ProtocolNumber,
		AddressWithPrefix: localAddr.WithPrefix(),
	}
	if err := s.AddProtocolAddress(1, protocolAddr, stack.AddressProperties{}); err != nil {
		return nil, fmt.Errorf("AddProtocolAddress(%d, %+v, {}): %s", 1, protocolAddr, err)
	}

	// Add default route.
	s.SetRouteTable([]tcpip.Route{
		{
			Destination: header.IPv4EmptySubnet,
			NIC:         1,
		},
	})

	return s, nil
}

func main() {
	_, err := createStack("10.0.1.2")
	if err != nil {
		panic(err)
	}

	// Block forever, or the WASM program quits.
	select {}
}
