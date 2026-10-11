package edge

import (
	"context"
	"log/slog"
	"net/netip"
	"regexp"
	"strings"
)

// addressPlaceholder takes the place of an address in a log line.
const addressPlaceholder = "[address]"

// ipv4Pattern matches an IPv4 address, with a port if there is one.
var ipv4Pattern = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}(?::\d{1,5})?\b`)

// ipv6Pattern matches what may be an IPv6 address, in brackets and with a port if there are any. It needs at least two
// colons, so a plain "host:port" is left alone. A clock time such as 12:34:56 matches it too, so [isIPv6] decides.
var ipv6Pattern = regexp.MustCompile(`(?i)\[?(?:[0-9a-f]{0,4}:){2,7}[0-9a-f]{0,4}(?:%[a-z0-9]+)?\]?(?::\d{1,5})?`)

// RemoveAddresses returns text with every IPv4 and IPv6 address, and its port, replaced by "[address]".
func RemoveAddresses(text string) string {
	text = ipv4Pattern.ReplaceAllString(text, addressPlaceholder)
	return ipv6Pattern.ReplaceAllStringFunc(text, func(match string) string {
		if isIPv6(match) {
			return addressPlaceholder
		}
		return match
	})
}

// isIPv6 reports whether match, which [ipv6Pattern] found, parses as an IPv6 address. The brackets and the port are
// taken off first; a zone, such as %en0, is part of the address.
func isIPv6(match string) bool {
	address := match
	if strings.HasPrefix(address, "[") {
		end := strings.IndexByte(address, ']')
		if end < 0 {
			return false
		}
		address = address[1:end]
	}
	parsed, err := netip.ParseAddr(address)
	return err == nil && parsed.Is6()
}

var _ slog.Handler = &AddressHandler{}

// An AddressHandler passes log records on to Next with the network addresses removed from the message and from the text of every
// attribute. Wrapping the handler of a logger once covers everything that logs through it, including the libraries that
// the logger is given to.
type AddressHandler struct {
	// Next receives the records that are cleaned.
	Next slog.Handler
}

// Enabled reports whether Next handles records of the level.
func (h *AddressHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Next.Enabled(ctx, level)
}

// Handle passes record on to Next, with the addresses removed.
func (h *AddressHandler) Handle(ctx context.Context, record slog.Record) error {
	cleaned := slog.NewRecord(record.Time, record.Level, RemoveAddresses(record.Message), record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		cleaned.AddAttrs(clean(attr))
		return true
	})
	return h.Next.Handle(ctx, cleaned)
}

// WithAttrs returns a handler whose records carry attrs, with the addresses removed from them.
func (h *AddressHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cleaned := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		cleaned[i] = clean(attr)
	}
	return &AddressHandler{Next: h.Next.WithAttrs(cleaned)}
}

// WithGroup returns a handler that puts the attributes of its records in the group.
func (h *AddressHandler) WithGroup(name string) slog.Handler {
	return &AddressHandler{Next: h.Next.WithGroup(name)}
}

// clean returns attr with the addresses removed from its text. In a group it cleans every member, and an error becomes
// its cleaned text. Any other value, such as a number, is left as it is.
func clean(attr slog.Attr) slog.Attr {
	switch attr.Value.Kind() {
	case slog.KindString:
		return slog.String(attr.Key, RemoveAddresses(attr.Value.String()))
	case slog.KindGroup:
		group := attr.Value.Group()
		cleaned := make([]any, len(group))
		for i, member := range group {
			cleaned[i] = clean(member)
		}
		return slog.Group(attr.Key, cleaned...)
	case slog.KindAny:
		if err, ok := attr.Value.Any().(error); ok {
			return slog.String(attr.Key, RemoveAddresses(err.Error()))
		}
	}
	return attr
}
