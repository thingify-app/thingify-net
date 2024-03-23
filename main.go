package main

import (
	"log"

	"github.com/songgao/water"
	"github.com/vishvananda/netlink"
)

func main() {
	tun, err := water.New(water.Config{
		DeviceType: water.TUN,
		PlatformSpecificParams: water.PlatformSpecificParams{
			Name: "thingify0",
		},
	})

	if err != nil {
		panic(err)
	}

	link, err := netlink.LinkByName(tun.Name())
	if err != nil {
		panic(err)
	}

	addr, err := netlink.ParseAddr("10.0.1.0/24")
	if err != nil {
		panic(err)
	}

	err = netlink.AddrAdd(link, addr)
	if err != nil {
		panic(err)
	}

	err = netlink.LinkSetUp(link)
	if err != nil {
		panic(err)
	}

	buffer := make([]byte, 2000)
	for {
		n, err := tun.Read(buffer)
		if err != nil {
			panic(err)
		}
		log.Printf("Packet received: % x\n", buffer[:n])
	}
}
