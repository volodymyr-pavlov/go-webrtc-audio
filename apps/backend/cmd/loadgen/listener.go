package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
)

// A listener is one fake client. It records what it hears, and the fields are read by other goroutines only after
// [listener.stopped] is closed or through the atomics.
type listener struct {
	api      *webrtc.API
	base     string
	client   *http.Client
	location string
	pc       *webrtc.PeerConnection

	received atomic.Uint64
	lost     atomic.Uint64
	maxGap   atomic.Int64
	started  atomic.Int64
	failure  atomic.Pointer[error]
}

// connect sets up the connection and returns when the first packet has arrived or ctx is done.
func (l *listener) connect(ctx context.Context) error {
	begin := time.Now()
	pc, err := l.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return err
	}
	l.pc = pc
	firstCh := make(chan struct{})
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		l.read(track, begin, firstCh)
	})
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		return err
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return err
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		return err
	}
	select {
	case <-gathered:
	case <-ctx.Done():
		return fmt.Errorf("gathering candidates: %w", ctx.Err())
	}

	sdp := pc.LocalDescription().SDP
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, l.base+"/api/listen", strings.NewReader(sdp))
	if err != nil {
		return err
	}
	response, err := l.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	answer, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusCreated {
		return fmt.Errorf("%w: %s: %s", errServer, response.Status, strings.TrimSpace(string(answer)))
	}
	l.location = response.Header.Get("Location")
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  string(answer),
	}); err != nil {
		return err
	}
	select {
	case <-firstCh:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%w: %w", errNoTrack, ctx.Err())
	}
}

// read counts the packets of the track until the connection ends.
func (l *listener) read(track *webrtc.TrackRemote, begin time.Time, firstCh chan struct{}) {
	var expected uint16
	var last time.Time
	opened := false
	for {
		packet, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		now := time.Now()
		if !opened {
			opened = true
			l.started.Store(int64(now.Sub(begin)))
			close(firstCh)
		} else {
			if gap := packet.SequenceNumber - expected; gap != 0 && gap < 1000 {
				l.lost.Add(uint64(gap))
			}
			// The first second is the jitter buffer filling, not a silence.
			if now.Sub(begin) > time.Second+time.Duration(l.started.Load()) {
				if d := int64(now.Sub(last)); d > l.maxGap.Load() {
					l.maxGap.Store(d)
				}
			}
		}
		expected = packet.SequenceNumber + 1
		last = now
		l.received.Add(1)
	}
}

// disconnect tells the server and closes the connection.
func (l *listener) disconnect(ctx context.Context) {
	if l.location != "" {
		request, err := http.NewRequestWithContext(ctx, http.MethodDelete, l.base+"/api/"+l.location, nil)
		if err == nil {
			if response, err := l.client.Do(request); err == nil {
				_ = response.Body.Close() // The body is not used.
			}
		}
	}
	if l.pc != nil {
		_ = l.pc.Close() // The listener is done whether or not the close succeeds.
	}
}
