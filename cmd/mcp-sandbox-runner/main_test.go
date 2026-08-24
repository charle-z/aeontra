package main

import "testing"

func TestValidateListenAddressRejectsPublicOrUnspecifiedBinds(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8770", "10.0.0.2:8770", "[fd00::1]:8770", "localhost:8770"} {
		if err := validateListenAddress(address); err != nil {
			t.Errorf("private address %q was rejected: %v", address, err)
		}
	}
	for _, address := range []string{":8770", "0.0.0.0:8770", "[::]:8770", "8.8.8.8:8770", "public.example.invalid:8770"} {
		if err := validateListenAddress(address); err == nil {
			t.Errorf("unsafe address %q was accepted", address)
		}
	}
}
