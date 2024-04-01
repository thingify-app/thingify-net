//go:build js && wasm

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand"
	"net"
	"syscall/js"
	"time"

	"golang.org/x/crypto/ssh"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/link/sniffer"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
)

const LOCAL_HOST_IP = "10.0.1.2"
const MTU_BYTES = 1024

func createStack(localAddrName string) (*stack.Stack, error) {
	rand.Seed(time.Now().UnixNano())

	localAddr := tcpip.AddrFromSlice(net.ParseIP(localAddrName).To4())

	// Create the stack with ipv4 and tcp protocols, then add a tun-based
	// NIC and ipv4 address.
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol},
	})

	linkEP := channel.New(0, MTU_BYTES, "")
	if err := s.CreateNIC(1, sniffer.New(linkEP)); err != nil {
		return nil, fmt.Errorf("error creating NIC: %v", err)
	}

	incomingBuf := make([]byte, MTU_BYTES)
	incomingMessageBuffer := js.Global().Get("outgoingMessageBuffer")
	messageListener := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		go func() {
			bufLen := args[0].Int()
			js.CopyBytesToGo(incomingBuf, incomingMessageBuffer)
			fmt.Printf("binary message received (len %v): %v\n", bufLen, hex.EncodeToString(incomingBuf[:bufLen]))
			linkEP.InjectInbound(ipv4.ProtocolNumber, stack.NewPacketBuffer(stack.PacketBufferOptions{
				Payload: buffer.MakeWithData(incomingBuf[:bufLen]),
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
			bufLen := len(flatBuffer)
			fmt.Printf("Sending to JS (len %v): %v\n", bufLen, hex.EncodeToString(flatBuffer))
			js.CopyBytesToJS(messageBuffer, flatBuffer)
			js.Global().Call("sendToPeer", bufLen)
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

func dial(stack *stack.Stack, remoteAddrName string, remotePort uint16) (net.Conn, error) {
	remote := tcpip.FullAddress{
		NIC:  1,
		Addr: tcpip.AddrFromSlice(net.ParseIP(remoteAddrName).To4()),
		Port: remotePort,
	}

	return gonet.DialTCP(stack, remote, ipv4.ProtocolNumber)
}

func createSshSession(stack *stack.Stack, host string, username string, password string) error {
	conn, err := dial(stack, host, 22)
	if err != nil {
		return err
	}

	auth := []ssh.AuthMethod{ssh.Password(password)}
	clientConfig := &ssh.ClientConfig{
		User:            username,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	sshConn, nc, r, err := ssh.NewClientConn(conn, host, clientConfig)
	if err != nil {
		return err
	}
	client := ssh.NewClient(sshConn, nc, r)
	session, err := client.NewSession()
	if err != nil {
		return err
	}

	stdIn, err := session.StdinPipe()
	if err != nil {
		return err
	}
	forwardInStream(stdIn)

	stdOut, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	forwardOutStream(stdOut)

	stdErr, err := session.StderrPipe()
	if err != nil {
		return err
	}

	forwardOutStream(stdErr)

	modes := ssh.TerminalModes{
		ssh.ECHO:          1, // disable echoing
		ssh.ICRNL:         1,
		ssh.IXON:          1,
		ssh.IXANY:         1,
		ssh.IMAXBEL:       1,
		ssh.OPOST:         1,
		ssh.ONLCR:         1,
		ssh.ISIG:          1,
		ssh.ICANON:        1,
		ssh.IEXTEN:        1,
		ssh.ECHOE:         1,
		ssh.ECHOK:         1,
		ssh.ECHOCTL:       1,
		ssh.ECHOKE:        1,
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}

	err = session.RequestPty("xterm", 40, 80, modes)
	if err != nil {
		return err
	}
	err = session.Shell()
	if err != nil {
		return err
	}

	err = session.Wait()
	if err != nil {
		return err
	}

	err = session.Close()
	if err != nil {
		return err
	}
	return nil
}

func forwardOutStream(r io.Reader) {
	go func() {
		buf := make([]byte, 2048)
		for {
			n, err := r.Read(buf)
			if err != nil {
				panic(err)
			}
			writeToConsole(string(buf[:n]))
		}
	}()
}

func forwardInStream(w io.Writer) {
	stdInListener := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		go func() {
			stdIn := args[0].String()
			_, err := w.Write([]byte(stdIn))
			if err != nil {
				panic(err)
			}
		}()
		return nil
	})

	js.Global().Set("stdInListener", stdInListener)
}

func writeToConsole(str string) {
	js.Global().Call("writeToConsole", str)
}

func main() {
	stack, err := createStack(LOCAL_HOST_IP)
	if err != nil {
		panic(err)
	}

	init := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		host, username, password := args[0].String(), args[1].String(), args[2].String()
		fmt.Printf("Connecting to host: %v with username: %v\n", host, username)
		go func() {
			err = createSshSession(stack, host, username, password)
			if err != nil {
				panic(err)
			}
		}()
		return nil
	})

	js.Global().Set("init", init)

	fmt.Println("WASM loaded.")

	select {}
}
