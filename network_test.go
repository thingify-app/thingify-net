//go:build linux

package main

import (
	"fmt"
	"net"
	"testing"
)

func TestConnect(t *testing.T) {
	listener, err := net.Listen("tcp", ":1234")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				t.Fatal(err)
			}
			resp := fmt.Sprintf("hello %v", conn.RemoteAddr().String())
			conn.Write([]byte(resp))
			conn.Close()
		}
	}()

	stack, err := CreateStack("thingifyTest", "10.0.1.2")
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		client, err := stack.LeaseClient()
		if err != nil {
			t.Fatal(err)
		}

		conn, err := client.DialTCP("10.0.1.1", 1234)
		if err != nil {
			t.Fatal(err)
		}

		buf := make([]byte, 100)
		n, err := conn.Read(buf)
		t.Logf("Read %v\n", string(buf[:n]))
		if err != nil {
			t.Fatal(err)
		}
	}
}
