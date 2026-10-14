package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunHelpListsOperationalSubcommands(t *testing.T) {
	var output bytes.Buffer
	if err := run(context.Background(), []string{"--help"}, &output); err != nil {
		t.Fatalf("run help failed: %v", err)
	}
	text := output.String()
	for _, want := range []string{"migrate", "serve", "doctor", "import-sqlite", "verify-sqlite"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help output missing %q: %s", want, text)
		}
	}
}

func TestBackendAddrRejectsBarePort(t *testing.T) {
	t.Setenv("CPA_HELPER_ADDR", "18317")
	if _, err := backendAddr(); err == nil {
		t.Fatal("backendAddr accepted a bare port")
	}
}

func TestTransferCommandsRequireExplicitSource(t *testing.T) {
	for _, command := range []string{"import-sqlite", "verify-sqlite"} {
		var output bytes.Buffer
		if err := run(context.Background(), []string{command}, &output); err == nil || !strings.Contains(err.Error(), "--source") {
			t.Fatalf("%s accepted missing source: %v", command, err)
		}
	}
}
