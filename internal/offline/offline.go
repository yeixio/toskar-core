// Package offline tells a failure to reach the internet from other errors
// (#965), so the person is told this computer is offline, not shown
// "dial tcp: lookup huggingface.co: no such host", and a model told a
// look-up failed isn't left to answer from memory as if it had looked.
package offline

import (
	"errors"
	"net"
	"syscall"
)

// Code is the API error code for a request that needed the internet while
// this computer couldn't reach it (contract 1.92).
const Code = "OFFLINE"

// Is reports an error from not reaching the internet: a name that couldn't
// be looked up, or no route to the network. A server that answered with an
// error, or refused the connection, is online and isn't this.
func Is(err error) bool {
	if err == nil {
		return false
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return dns.IsNotFound || dns.IsTemporary || dns.IsTimeout
	}
	return errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETDOWN)
}

// Host is the name or address that couldn't be reached, when the error
// says.
func Host(err error) string {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return dns.Name
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Addr != nil {
		if host, _, splitErr := net.SplitHostPort(op.Addr.String()); splitErr == nil {
			return host
		}
		return op.Addr.String()
	}
	return ""
}

// Message is what the person reads for an error Is reports.
func Message(err error) string {
	if host := Host(err); host != "" {
		return "This computer isn't connected to the internet, so " + host + " couldn't be reached. Check the connection, then try again."
	}
	return "This computer isn't connected to the internet. Check the connection, then try again."
}
