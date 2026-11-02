package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerErrorLogDropsClientAddress(t *testing.T) {
	var logs bytes.Buffer
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("handler-sentinel")
	}))
	srv.Config.ErrorLog = serverErrorLog(slog.New(slog.NewTextHandler(&logs, nil)).Handler())
	srv.Start()

	resp, err := srv.Client().Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("request to a panicking handler succeeded")
	}
	srv.Close()

	got := logs.String()
	if !strings.Contains(got, "handler-sentinel") {
		t.Errorf("log lacks the panic: %q", got)
	}
	if strings.Contains(got, "127.0.0.1") {
		t.Errorf("log holds the client address: %q", got)
	}
}
