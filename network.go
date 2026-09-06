//go:build linux

package main

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/netip"
	"time"

	"github.com/vishvananda/netlink"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/fdbased"
	"gvisor.dev/gvisor/pkg/tcpip/link/tun"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

const MTU_BYTES = 16384

type NetworkStack struct {
	stack    *stack.Stack
	ipamPool *IPAMPool
}

func CreateStack(tunName string, startIp string) (*NetworkStack, error) {
	rand.Seed(time.Now().UnixNano())

	tunFd, err := setupTunInterface(tunName)
	if err != nil {
		return nil, err
	}

	// Create the stack with ipv4 and tcp/udp protocols, then add a tun-based
	// NIC and ipv4 address.
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})

	linkEP, err := fdbased.New(&fdbased.Options{
		FDs:            []int{tunFd},
		MTU:            MTU_BYTES,
		EthernetHeader: false,
	})

	if err != nil {
		return nil, fmt.Errorf("error creating link: %v", err)
	}

	if err := s.CreateNIC(1, linkEP); err != nil {
		return nil, fmt.Errorf("error creating NIC: %v", err)
	}

	// Add default route.
	s.SetRouteTable([]tcpip.Route{
		{
			Destination: header.IPv4EmptySubnet,
			NIC:         1,
		},
	})

	pool, err := NewIPAMPool(startIp, 10)
	if err != nil {
		return nil, fmt.Errorf("error creating IP pool: %v", err)
	}

	return &NetworkStack{
		stack:    s,
		ipamPool: pool,
	}, nil
}

type Client struct {
	stack       *NetworkStack
	localAddr   netip.Addr
	fullAddress tcpip.FullAddress
}

func (s NetworkStack) LeaseClient() (*Client, error) {
	ip, err := s.ipamPool.Lease()
	if err != nil {
		return nil, err
	}

	localAddr := tcpip.AddrFromSlice(ip.AsSlice())

	protocolAddr := tcpip.ProtocolAddress{
		Protocol:          ipv4.ProtocolNumber,
		AddressWithPrefix: localAddr.WithPrefix(),
	}
	if err := s.stack.AddProtocolAddress(1, protocolAddr, stack.AddressProperties{}); err != nil {
		return nil, fmt.Errorf("AddProtocolAddress(%d, %+v, {}): %s", 1, protocolAddr, err)
	}

	return &Client{
		stack:     &s,
		localAddr: ip,
		fullAddress: tcpip.FullAddress{
			NIC:  1,
			Addr: localAddr,
		},
	}, nil
}

func (c *Client) DialTCP(remoteAddrName string, remotePort uint16) (net.Conn, error) {
	remote := parseAddr(remoteAddrName, remotePort)
	ctx := context.Background()
	return gonet.DialTCPWithBind(ctx, c.stack.stack, c.fullAddress, remote, ipv4.ProtocolNumber)
}

func (c *Client) DialUDP(remoteAddrName string, remotePort uint16) (net.Conn, error) {
	remote := parseAddr(remoteAddrName, remotePort)
	return gonet.DialUDP(c.stack.stack, &c.fullAddress, &remote, ipv4.ProtocolNumber)
}

func (c *Client) Release() {
	c.stack.ipamPool.Release(c.localAddr)
}

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

func parseAddr(addr string, port uint16) tcpip.FullAddress {
	return tcpip.FullAddress{
		NIC:  1,
		Addr: tcpip.AddrFromSlice(net.ParseIP(addr).To4()),
		Port: port,
	}
}
