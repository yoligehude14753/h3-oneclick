package detect

import "testing"

func TestParseNVIDIAOutput(t *testing.T) {
	items := ParseNVIDIAOutput("NVIDIA RTX 4090, 24564, 23100, 8.9\nNVIDIA RTX 3060, 12288, 9000, 8.6")
	if len(items) != 2 {
		t.Fatalf("expected two GPUs, got %d", len(items))
	}
	if items[0].Name != "NVIDIA RTX 4090" || items[0].VRAMGiB < 23.9 || items[0].ComputeCap != "8.9" {
		t.Fatalf("unexpected first GPU: %#v", items[0])
	}
}

func TestParsePort(t *testing.T) {
	if got := ParsePort("run --listen 127.0.0.1 --port=8189"); got != 8189 {
		t.Fatalf("expected 8189, got %d", got)
	}
}
