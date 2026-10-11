package edge

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestRemoveAddressesReplacesIPv4IPv6AndPorts(t *testing.T) {
	for text, want := range map[string]string{
		`error when serving connection "10.0.0.5:8080"<->"203.0.113.7:51234": EOF`: `error when serving connection "[address]"<->"[address]": EOF`,
		`from 2001:db8::1 and [2001:db8:0:1::2]:5005 and fe80::1%en0`:              `from [address] and [address] and [address]`,
		`no address here, host:port stays`:                                         `no address here, host:port stays`,
		`version 1.2.3 and 12 listeners`:                                           `version 1.2.3 and 12 listeners`,
		`at 12:34:56 the model failed, deadline 2026-10-09T12:00:00Z`:              `at 12:34:56 the model failed, deadline 2026-10-09T12:00:00Z`,
		`mac aa:bb:cc:dd:ee:ff, but ::1 and [::1]:8080 and a::b are addresses`:     `mac aa:bb:cc:dd:ee:ff, but [address] and [address] and [address] are addresses`,
		``: ``,
	} {
		if got := RemoveAddresses(text); got != want {
			t.Errorf("RemoveAddresses(%q)\n got %q\nwant %q", text, got, want)
		}
	}
}

func TestAddressHandlerRemovesAddressesFromTheMessageAndFromEveryKindOfAttribute(t *testing.T) {
	var out strings.Builder
	logger := slog.New(&AddressHandler{Next: slog.NewTextHandler(&out, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return attr
		},
	})}).With("peer", "203.0.113.7:5005")

	logger.Error("lost 198.51.100.9:443", "cause", errors.New("write to [2001:db8::1]:5005: closed"), "n", 3,
		slog.Group("g", slog.String("from", "10.1.2.3")))

	got := out.String()
	for _, leaked := range []string{"203.0.113.7", "198.51.100.9", "2001:db8", "10.1.2.3"} {
		if strings.Contains(got, leaked) {
			t.Errorf("the address %s is in the log: %s", leaked, got)
		}
	}
	if !strings.Contains(got, "n=3") || !strings.Contains(got, "lost [address]") {
		t.Errorf("the rest of the record was changed: %s", got)
	}
}
