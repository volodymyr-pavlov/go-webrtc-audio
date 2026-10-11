package api

import (
	"errors"
	"strings"
	"testing"

	"github.com/valyala/fasthttp"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/turn"
)

// A handler is a struct literal, so a test builds it with only what it needs and calls it without the mux.

func TestICEHandlerAnswersAnEmptyListWithoutTURN(t *testing.T) {
	h := &ICEHandler{}
	var ctx fasthttp.RequestCtx
	if err := h.ServeHTTP(&ctx); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(ctx.Response.Body())); ctx.Response.StatusCode() != fasthttp.StatusOK || got != `{"data":{"iceServers":[]}}` {
		t.Fatalf("got %d %s", ctx.Response.StatusCode(), got)
	}
}

func TestICEHandlerHandsOutTheCredentialsOfTURN(t *testing.T) {
	issuer, err := turn.New(turn.Options{
		URLs:   []string{"turn:turn.example.com:3478"},
		Secret: []byte("0123456789abcdef-secret"),
	})
	if err != nil {
		t.Fatal(err)
	}
	h := &ICEHandler{TURN: issuer}
	var ctx fasthttp.RequestCtx
	if err := h.ServeHTTP(&ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ctx.Response.Body()), `"turn:turn.example.com:3478"`) {
		t.Fatalf("got %s", ctx.Response.Body())
	}
}

func TestLeaveHandlerArgsRefuseAnIDThatIsNotANumber(t *testing.T) {
	var ctx fasthttp.RequestCtx
	var args LeaveHandlerArgs
	ctx.SetUserValue("id", "abc")
	if err := args.Parse(&ctx); !errors.Is(err, ErrBadListenerID) {
		t.Fatalf("got %v, want ErrBadListenerID", err)
	}
	ctx.SetUserValue("id", "42")
	if err := args.Parse(&ctx); err != nil || args.ID != 42 {
		t.Fatalf("got %d %v, want 42", args.ID, err)
	}
}

func TestListenHandlerArgsTakeTheBodyAsTheOffer(t *testing.T) {
	var ctx fasthttp.RequestCtx
	ctx.Request.SetBodyString("v=0\n")
	var args ListenHandlerArgs
	if err := args.Parse(&ctx); err != nil || args.Offer != "v=0\n" {
		t.Fatalf("got %q %v", args.Offer, err)
	}
}
