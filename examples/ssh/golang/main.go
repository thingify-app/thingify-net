//go:build js && wasm

package main

import (
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"syscall/js"

	"golang.org/x/crypto/ssh"
)

const BUFFER_SIZE_BYTES = 16384

var incomingNetworkBuffer = make([]byte, BUFFER_SIZE_BYTES)
var outgoingNetworkBuffer = make([]byte, BUFFER_SIZE_BYTES)

var incomingTerminalBuffer = make([]byte, BUFFER_SIZE_BYTES)
var outgoingTerminalBuffer = make([]byte, BUFFER_SIZE_BYTES)

var stdInWriter io.Writer
var networkWriter io.Writer

func createSshSession(conn net.Conn, host string, username string, password string) error {
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
		for {
			n, err := r.Read(outgoingTerminalBuffer)
			if err != nil {
				fmt.Printf("Error writing to terminal: %v\n", err)
			}
			sendToTerminal(n)
		}
	}()
}

func forwardInStream(w io.Writer) {
	stdInWriter = w
}

func dataChannelToConn() net.Conn {
	dcConn, sshConn := net.Pipe()

	go func() {
		for {
			n, err := dcConn.Read(outgoingNetworkBuffer)
			if err != nil {
				fmt.Printf("Error writing to network: %v\n", err)
				break
			}
			fmt.Printf("Sending to JS (len %v): %v\n", n, hex.EncodeToString(outgoingNetworkBuffer[:n]))
			sendToNetwork(n)
		}
	}()

	// Allow writing to the conn when we receive bytes from the network.
	networkWriter = dcConn

	return sshConn
}

// This is needed to wrap any exported function which calls a goroutine.
// Not really sure why!
func wrapFunc(f func()) {
	js.FuncOf(func(this js.Value, args []js.Value) any {
		f()
		return nil
	}).Invoke()
}

//export getBufferSize
func getBufferSize() int {
	return BUFFER_SIZE_BYTES
}

//export getIncomingNetworkBuffer
func getIncomingNetworkBuffer() *byte {
	return &incomingNetworkBuffer[0]
}

//export getOutgoingNetworkBuffer
func getOutgoingNetworkBuffer() *byte {
	return &outgoingNetworkBuffer[0]
}

//export getIncomingTerminalBuffer
func getIncomingTerminalBuffer() *byte {
	return &incomingTerminalBuffer[0]
}

//export getOutgoingTerminalBuffer
func getOutgoingTerminalBuffer() *byte {
	return &outgoingTerminalBuffer[0]
}

//export sendToNetwork
func sendToNetwork(n int)

//export receiveFromNetwork
func receiveFromNetwork(n int) {
	wrapFunc(func() {
		go func() {
			if networkWriter != nil {
				fmt.Printf("Received %v bytes: %v\n", n, hex.EncodeToString(incomingNetworkBuffer[:n]))
				_, err := networkWriter.Write(incomingNetworkBuffer[:n])
				if err != nil {
					fmt.Printf("Error writing to SSH conn: %v\n", err)
				}
			}
		}()
	})
}

//export sendToTerminal
func sendToTerminal(n int)

//export receiveFromTerminal
func receiveFromTerminal(n int) {
	wrapFunc(func() {
		go func() {
			if stdInWriter != nil {
				_, err := stdInWriter.Write(incomingTerminalBuffer[:n])
				if err != nil {
					fmt.Printf("Error writing to SSH session: %v\n", err)
				}
			}
		}()
	})
}

//export connect
func connect(host, username, password string) {
	wrapFunc(func() {
		fmt.Printf("Connecting to host: %v with username: %v\n", host, username)

		conn := dataChannelToConn()

		go func() {
			err := createSshSession(conn, host, username, password)
			if err != nil {
				fmt.Printf("Error creating SSH session: %v\n", err)
				return
			}
		}()
	})
}

func main() {
	fmt.Println("WASM loaded.")

	select {}
}
