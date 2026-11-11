package main

import (
	"errors"
	"testing"

	"github.com/gotd/td/tgerr"
)

func TestFloodWaitSeconds(t *testing.T) {
	wait, ok := floodWaitSeconds(&tgerr.Error{Code: 420, Type: "FLOOD_WAIT", Argument: 85})
	if !ok || wait != 85 {
		t.Fatalf("got %d,%v", wait, ok)
	}
	if _, ok := floodWaitSeconds(&tgerr.Error{Code: 400, Type: "CHANNEL_INVALID"}); ok {
		t.Fatal("fatal error classified as flood wait")
	}
	if _, ok := floodWaitSeconds(errors.New("boom")); ok {
		t.Fatal("plain error classified as flood wait")
	}
	if _, ok := floodWaitSeconds(nil); ok {
		t.Fatal("nil classified as flood wait")
	}
}
