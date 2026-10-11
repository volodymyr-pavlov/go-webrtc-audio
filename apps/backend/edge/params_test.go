package edge

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/valyala/fasthttp"
)

var errNotFoundThing = &Error{
	Status:   fasthttp.StatusNotFound,
	Num:      900,
	Code:     "NOT_FOUND_THING",
	Message:  "The thing is not found.",
	Template: "The thing %(id)s is not found.",
}

func TestWithRendersTheTemplateAndKeepsTheIdentity(t *testing.T) {
	err := errNotFoundThing.With(Params{"id": 42})

	if err.Message != "The thing 42 is not found." || !errors.Is(err, errNotFoundThing) {
		t.Fatalf("got %q", err.Message)
	}
	if errNotFoundThing.Message != "The thing is not found." || errNotFoundThing.Params != nil {
		t.Fatal("With changed the original")
	}
}

func TestWithKeepsTheMessageWhenAParamIsMissing(t *testing.T) {
	err := errNotFoundThing.With(Params{"other": 1})

	if err.Message != "The thing is not found." || err.Params["other"] != 1 {
		t.Fatalf("got %q %v", err.Message, err.Params)
	}
}

func TestWithReturnsTheErrorItselfWithoutParams(t *testing.T) {
	if errNotFoundThing.With(nil) != errNotFoundThing {
		t.Fatal("With made a copy for no params")
	}
}

func TestParamsStringIsSortedByKey(t *testing.T) {
	if got := (Params{
		"b": 2,
		"a": "x",
	}).String(); got != "a=x b=2" {
		t.Fatalf("got %q", got)
	}
}

func TestWithTakesTheParamsOfAStructUnderTheirJSONNames(t *testing.T) {
	type thing struct {
		ID     int    `json:"id"`
		Name   string `json:"name,omitempty"`
		Secret string `json:"-"`
		secret string
	}

	err := errNotFoundThing.With(thing{
		ID:     7,
		Secret: "x",
		secret: "y",
	})

	if err.Message != "The thing 7 is not found." || len(err.Params) != 1 || err.Params["id"] != 7 {
		t.Fatalf("got %q %v", err.Message, err.Params)
	}
	if errNotFoundThing.With(&thing{ID: 8}).Params["id"] != 8 {
		t.Fatal("a pointer to a struct was not read")
	}
	if errNotFoundThing.With(42) != errNotFoundThing {
		t.Fatal("something that is not a struct made params")
	}
}

func TestInterpolateRendersEveryPlaceholder(t *testing.T) {
	cases := []struct {
		template string
		params   Params
		want     string
		ok       bool
	}{
		{"", Params{"id": 1}, "", false},
		{"plain", nil, "plain", true},
		{"%(a)s and %(b)s", Params{
			"a": 1,
			"b": "two",
		}, "1 and two", true},
		{"%(a)s%(a)s", Params{"a": "x"}, "xx", true},
		{"%(a)s", Params{"b": 1}, "", false},
		{"open %(a", Params{"a": 1}, "open %(a", true},
		{"no suffix %(a)", Params{"a": 1}, "no suffix %(a)", true},
	}
	for _, c := range cases {
		if got, ok := interpolate(c.template, c.params); got != c.want || ok != c.ok {
			t.Errorf("interpolate(%q, %v) = %q, %v, want %q, %v", c.template, c.params, got, ok, c.want, c.ok)
		}
	}
}

func TestParamsStringOfNoParamsIsEmpty(t *testing.T) {
	if got := Params(nil).String(); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestParamsStringAndInterpolateDoNotShareThePooledBytes(t *testing.T) {
	var wg sync.WaitGroup
	for g := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 500 {
				params := Params{
					"g": g,
					"i": i,
				}
				if got, want := params.String(), fmt.Sprintf("g=%d i=%d", g, i); got != want {
					t.Errorf("got %q, want %q", got, want)
					return
				}
				got, ok := interpolate("%(g)s/%(i)s", params)
				if want := fmt.Sprintf("%d/%d", g, i); !ok || got != want {
					t.Errorf("got %q, want %q", got, want)
					return
				}
			}
		}()
	}
	wg.Wait()
}
