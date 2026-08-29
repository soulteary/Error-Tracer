package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeHTTPServer struct {
	shutdownErr error
	closeErr    error
	closed      bool
}

func (s *fakeHTTPServer) Shutdown(context.Context) error {
	return s.shutdownErr
}

func (s *fakeHTTPServer) Close() error {
	s.closed = true
	return s.closeErr
}

func TestStopHTTPServerGraceful(t *testing.T) {
	server := &fakeHTTPServer{}
	if err := stopHTTPServer(server, time.Second); err != nil {
		t.Fatalf("stopHTTPServer() error = %v", err)
	}
	if server.closed {
		t.Fatal("Close() called after a successful graceful shutdown")
	}
}

func TestStopHTTPServerForcesClose(t *testing.T) {
	shutdownErr := errors.New("shutdown timed out")
	server := &fakeHTTPServer{shutdownErr: shutdownErr}
	if err := stopHTTPServer(server, time.Second); !errors.Is(err, shutdownErr) {
		t.Fatalf("stopHTTPServer() error = %v, want %v", err, shutdownErr)
	}
	if !server.closed {
		t.Fatal("Close() was not called after graceful shutdown failed")
	}
}

func TestStopHTTPServerReportsCloseFailure(t *testing.T) {
	shutdownErr := errors.New("shutdown timed out")
	closeErr := errors.New("close failed")
	server := &fakeHTTPServer{shutdownErr: shutdownErr, closeErr: closeErr}
	err := stopHTTPServer(server, time.Second)
	if !errors.Is(err, shutdownErr) || !errors.Is(err, closeErr) {
		t.Fatalf("stopHTTPServer() error = %v, want both shutdown and close errors", err)
	}
}
