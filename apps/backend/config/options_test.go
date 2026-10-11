package config

import (
	"errors"
	"testing"
	"time"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/engine"
)

// validOptions are settings that break no rule.
func validOptions() Options {
	return Options{
		HTTPAddr: ":8080",
		AudioDir: "audio",
		ICEPort:  5005,
		Mode:     engine.ModeFanout,
	}
}

func TestValidateAcceptsSettingsThatBreakNoRule(t *testing.T) {
	if err := validOptions().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateJoinsEveryRuleThatIsBroken(t *testing.T) {
	err := Options{
		ICEPort:    70000,
		Mode:       engine.Mode(99),
		Shards:     -1,
		Batch:      -1,
		BatchDelay: -time.Second,
	}.Validate()

	for _, want := range []error{ErrNoHTTPAddr, ErrNoAudioDir, ErrICEPort, ErrMode, ErrNegative} {
		if !errors.Is(err, want) {
			t.Errorf("the error does not say %v: %v", want, err)
		}
	}
}

func TestValidateRefusesATurnAddressWithoutASecretAndTheOtherWayAround(t *testing.T) {
	withURL := validOptions()
	withURL.TURNURLs = "turn:turn.example.com"
	withSecret := validOptions()
	withSecret.TURNSecretFile = "/nowhere"

	if err := withURL.Validate(); !errors.Is(err, ErrTURNNeedsSecret) {
		t.Errorf("an address without a secret: %v", err)
	}
	if err := withSecret.Validate(); !errors.Is(err, ErrTURNNeedsURL) {
		t.Errorf("a secret without an address: %v", err)
	}
}

func TestFlagsBecomeTheFlagsOfTheEngine(t *testing.T) {
	opts := Options{Flags: FlagLoopback | FlagProfiling | FlagPresenter}

	if opts.engineFlags() != engine.FlagLoopback {
		t.Errorf("engine flags %v", opts.engineFlags())
	}
	if (Options{}).engineFlags() != 0 {
		t.Error("no setting must give no flag")
	}
}

func TestPublicIPsSplitsTheHostsOnCommas(t *testing.T) {
	if got := (Options{ICEHost: "203.0.113.7,203.0.113.8"}).publicIPs(); len(got) != 2 || got[1] != "203.0.113.8" {
		t.Fatalf("got %v", got)
	}
	if (Options{}).publicIPs() != nil {
		t.Fatal("no host must give no address")
	}
}

func TestValidateRefusesWhatThePartsWouldReplaceWithTheirDefaults(t *testing.T) {
	tests := map[string]struct {
		change func(*Options)
		want   error
	}{
		"a negative connect timeout": {func(o *Options) { o.ConnectTimeout = -time.Second }, ErrNegative},
		"a negative evict time":      {func(o *Options) { o.EvictAfter = -time.Second }, ErrNegative},
		"a batch delay as long as the frame": {func(o *Options) {
			o.Batch = 64
			o.Frame = 20 * time.Millisecond
			o.BatchDelay = 20 * time.Millisecond
		}, ErrBatchDelay},
		"a TURN TTL of zero": {func(o *Options) {
			o.TURNURLs = "turn:turn.example.com"
			o.TURNSecretFile = "secret"
			o.TURNTTL = 0
		}, ErrTURNTTL},
	}
	for name, test := range tests {
		opts := validOptions()
		test.change(&opts)
		if err := opts.Validate(); !errors.Is(err, test.want) {
			t.Errorf("%s: got %v, want %v", name, err, test.want)
		}
	}
	opts := validOptions()
	opts.Batch = 64
	opts.Frame = 20 * time.Millisecond
	opts.BatchDelay = 2 * time.Millisecond
	if err := opts.Validate(); err != nil {
		t.Errorf("a batch delay shorter than the frame: %v", err)
	}
}
