package app

import (
	"bytes"
	"context"
	"testing"
)

func TestCLIParsesBeforeOpeningDataDirectory(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"db"}, {"setup"}, {""}, {"serve", "--unknown"}} {
		var out bytes.Buffer
		if err := Run(context.Background(), args, "test", &out, &out); err == nil {
			t.Errorf("accepted invalid command %q", args)
		}
	}
	for _, args := range [][]string{{"version"}, {"--help"}} {
		var out bytes.Buffer
		if err := Run(context.Background(), args, "test", &out, &out); err != nil {
			t.Error(err)
		}
		if out.Len() == 0 {
			t.Fatal("missing CLI output")
		}
	}
}
