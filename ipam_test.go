package main

import "testing"

func TestIPOrder(t *testing.T) {
	pool, err := NewIPAMPool("10.0.1.1", 2)
	if err != nil {
		t.Fatal(err)
	}

	ip, err := pool.Lease()
	if err != nil {
		t.Fatal(err)
	}

	if ip.String() != "10.0.1.1" {
		t.Errorf("Expected first IP to be 10.0.1.1, was %v", ip.String())
	}

	ip, err = pool.Lease()
	if err != nil {
		t.Fatal(err)
	}

	if ip.String() != "10.0.1.2" {
		t.Errorf("Expected second IP to be 10.0.1.2, was %v", ip.String())
	}
}

func TestRelease(t *testing.T) {
	pool, err := NewIPAMPool("10.0.1.1", 2)
	if err != nil {
		t.Fatal(err)
	}

	ip1, err := pool.Lease()
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Lease()
	if err != nil {
		t.Fatal(err)
	}

	pool.Release(ip1)

	ip3, err := pool.Lease()
	if err != nil {
		t.Fatal(err)
	}

	if ip3.String() != "10.0.1.1" {
		t.Errorf("Expected third IP to be 10.0.1.1, after release was %v", ip3.String())
	}
}

func TestPoolExhausted(t *testing.T) {
	pool, err := NewIPAMPool("10.0.1.1", 2)
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Lease()
	if err != nil {
		t.Errorf("Expected success on 1st lease")
	}

	_, err = pool.Lease()
	if err != nil {
		t.Errorf("Expected success on 2nd lease")
	}

	_, err = pool.Lease()
	if err == nil {
		t.Errorf("Expected error on 3rd lease")
	}
}
