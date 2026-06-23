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

func dataChannelToConn(incomingMessageBuffer, outgoingMessageBuffer js.Value) net.Conn {
	dcConn, sshConn := net.Pipe()

	go func() {
		outgoingBuf := make([]byte, 16384)
		for {
			n, err := dcConn.Read(outgoingBuf)
			if err != nil {
				panic(err)
			}
			fmt.Printf("Sending to JS (len %v): %v\n", n, hex.EncodeToString(outgoingBuf[:n]))
			js.CopyBytesToJS(outgoingMessageBuffer, outgoingBuf[:n])
			js.Global().Call("sendToPeer", n)
		}
	}()

	messageListener := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		go func() {
			bufLen := args[0].Int()
			incomingBuf := make([]byte, bufLen)
			js.CopyBytesToGo(incomingBuf, incomingMessageBuffer)
			fmt.Printf("binary message received (len %v): %v\n", bufLen, hex.EncodeToString(incomingBuf[:bufLen]))
			dcConn.Write(incomingBuf)
		}()
		return nil
	})

	js.Global().Set("messageListener", messageListener)

	return sshConn
}

func main() {
	init := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		host, username, password := args[0].String(), args[1].String(), args[2].String()
		fmt.Printf("Connecting to host: %v with username: %v\n", host, username)

		incomingMessageBuffer := js.Global().Get("outgoingMessageBuffer")
		outgoingMessageBuffer := js.Global().Get("messageBuffer")
		conn := dataChannelToConn(incomingMessageBuffer, outgoingMessageBuffer)

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
