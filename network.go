package main

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"time"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/link/sniffer"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

type NetworkStack struct {
	stack     *stack.Stack
	localAddr tcpip.FullAddress
}

func createStack(localAddrName string, localTun io.ReadWriter) (*NetworkStack, error) {
	rand.Seed(time.Now().UnixNano())

	localAddr := tcpip.AddrFromSlice(net.ParseIP(localAddrName).To4())

	// Create the stack with ipv4 and tcp/udp protocols, then add a tun-based
	// NIC and ipv4 address.
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})

	linkEP := channel.New(0, MTU_BYTES, "")
	if err := s.CreateNIC(1, sniffer.New(linkEP)); err != nil {
		return nil, fmt.Errorf("error creating NIC: %v", err)
	}

	// Read packets from the local interface and write into the stack.
	incomingBuf := make([]byte, MTU_BYTES)
	go func() {
		for {
			n, err := localTun.Read(incomingBuf)
			if err != nil {
				panic("Error")
			}
			linkEP.InjectInbound(ipv4.ProtocolNumber, stack.NewPacketBuffer(stack.PacketBufferOptions{
				Payload: buffer.MakeWithData(incomingBuf[:n]),
			}))
		}
	}()

	// Write packets to the local interface, from the stack.
	go func() {
		for {
			packetBuffer := linkEP.ReadContext(context.Background())
			buffer := packetBuffer.ToBuffer()
			flatBuffer := buffer.Flatten()
			localTun.Write(flatBuffer)
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

	return &NetworkStack{
		stack: s,
		localAddr: tcpip.FullAddress{
			NIC:  1,
			Addr: localAddr,
		},
	}, nil
}

func (s NetworkStack) DialTCP(remoteAddrName string, remotePort uint16) (net.Conn, error) {
	remote := parseAddr(remoteAddrName, remotePort)
	return gonet.DialTCP(s.stack, remote, ipv4.ProtocolNumber)
}

func (s NetworkStack) DialUDP(remoteAddrName string, remotePort uint16) (net.Conn, error) {
	remote := parseAddr(remoteAddrName, remotePort)
	return gonet.DialUDP(s.stack, &s.localAddr, &remote, ipv4.ProtocolNumber)
}

func parseAddr(addr string, port uint16) tcpip.FullAddress {
	return tcpip.FullAddress{
		NIC:  1,
		Addr: tcpip.AddrFromSlice(net.ParseIP(addr).To4()),
		Port: port,
	}
}
