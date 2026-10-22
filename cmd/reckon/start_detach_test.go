package main

import (
	"testing"
	"time"
)

func TestParseStartFlags(t *testing.T) {
	o, err := parseStartFlags(nil)
	if err != nil || o.detach || o.timeout != 3*time.Minute {
		t.Fatalf("defaults = %+v, %v; want foreground, 3m", o, err)
	}
	o, err = parseStartFlags([]string{"--detach", "--timeout", "10m"})
	if err != nil || !o.detach || o.timeout != 10*time.Minute {
		t.Fatalf("--detach --timeout 10m = %+v, %v", o, err)
	}
	if o, err := parseStartFlags([]string{"-d"}); err != nil || !o.detach {
		t.Fatalf("-d = %+v, %v; want detach", o, err)
	}
	if _, err := parseStartFlags([]string{"extra"}); err == nil {
		t.Error("positional argument: want an error")
	}
}
