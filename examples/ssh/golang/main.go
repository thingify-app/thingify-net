//go:build js && wasm

package main

import (
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"syscall/js"
	"time"

	"golang.org/x/crypto/ssh"
)

// Port is made-up here, doesn't seem to matter to SSH but we should investigate.
const LOCAL_HOST_IP = "10.0.1.2:3000"

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

type dcRwc struct {
	outgoingMessageBuffer js.Value
	readChan              chan byte
}

func (d *dcRwc) Read(p []byte) (n int, err error) {
	fmt.Printf("Reading into buffer of size %v...\n", len(p))

	// Reading only one byte at a time - we should find a more efficient way to
	// do this.
	buf := <-d.readChan
	p[0] = buf
	return 1, nil
}

func (d *dcRwc) Write(p []byte) (n int, err error) {
	n = len(p)
	fmt.Printf("Sending to JS (len %v): %v\n", n, hex.EncodeToString(p))
	js.CopyBytesToJS(d.outgoingMessageBuffer, p)
	js.Global().Call("sendToPeer", n)
	return n, nil
}

func (dcRwc) Close() error {
	return nil
}

func dataChannelToStream(incomingMessageBuffer, outgoingMessageBuffer js.Value) io.ReadWriteCloser {
	readChan := make(chan byte)
	rwc := &dcRwc{
		outgoingMessageBuffer: outgoingMessageBuffer,
		readChan:              readChan,
	}

	messageListener := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		go func() {
			bufLen := args[0].Int()
			incomingBuf := make([]byte, bufLen)
			js.CopyBytesToGo(incomingBuf, incomingMessageBuffer)
			fmt.Printf("binary message received (len %v): %v\n", bufLen, hex.EncodeToString(incomingBuf[:bufLen]))
			for _, b := range incomingBuf {
				readChan <- b
			}
		}()
		return nil
	})

	js.Global().Set("messageListener", messageListener)

	return rwc
}

type rwcConn struct {
	io.ReadWriteCloser
	localAddr  net.Addr
	remoteAddr net.Addr
}

func NewConn(rwc io.ReadWriteCloser, local, remote net.Addr) net.Conn {
	return &rwcConn{
		ReadWriteCloser: rwc,
		localAddr:       local,
		remoteAddr:      remote,
	}
}

// LocalAddr returns the local network address.
func (c *rwcConn) LocalAddr() net.Addr {
	return c.localAddr
}

// RemoteAddr returns the remote network address.
func (c *rwcConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

// SetDeadline implements the net.Conn SetDeadline method.
// Standard io.ReadWriteCloser streams do not support deadlines, so we return nil.
func (c *rwcConn) SetDeadline(t time.Time) error {
	return nil
}

// SetReadDeadline implements the net.Conn SetReadDeadline method.
func (c *rwcConn) SetReadDeadline(t time.Time) error {
	return nil
}

// SetWriteDeadline implements the net.Conn SetWriteDeadline method.
func (c *rwcConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func main() {
	init := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		host, username, password := args[0].String(), args[1].String(), args[2].String()
		fmt.Printf("Connecting to host: %v with username: %v\n", host, username)

		localAddr, err := net.ResolveTCPAddr("tcp", LOCAL_HOST_IP)
		if err != nil {
			panic(err)
		}

		remoteAddr, err := net.ResolveTCPAddr("tcp", host)
		if err != nil {
			panic(err)
		}

		incomingMessageBuffer := js.Global().Get("outgoingMessageBuffer")
		outgoingMessageBuffer := js.Global().Get("messageBuffer")
		rwc := dataChannelToStream(incomingMessageBuffer, outgoingMessageBuffer)
		conn := NewConn(rwc, localAddr, remoteAddr)

		go func() {
			err := createSshSession(conn, host, username, password)
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
