package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"Agro-Pulse/internal/demo"
)

func TestDemoCLIAndIsolation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--demo"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	var report demo.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Source != "synthetic_demo" || len(report.Scenarios) != 5 {
		t.Fatal("wrong demo output")
	}
	for _, args := range [][]string{{"--demo", "--iek-dir", "anything"}, {"--demo", "--as-of", "2026-01-01"}, {"--demo", "--months", "1"}} {
		if err := run(args, &stdout, &stderr); err == nil {
			t.Fatal("mixed CLI arguments accepted", args)
		}
	}
}
