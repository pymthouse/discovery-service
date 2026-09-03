package cache

import (
	"context"
	"testing"
	"time"

	"github.com/livepeer/discovery-service/pkg/discotypes"
)

func TestRawCacheRoundTripAndIsolation(t *testing.T) {
	layer, err := New(time.Minute, "", func() int64 { return 7 })
	if err != nil {
		t.Fatal(err)
	}
	in := []discotypes.WebhookOrchestrator{
		{
			Address:      "https://a.example",
			Score:        1,
			Capabilities: []string{"transcode/ffmpeg"},
		},
	}
	ctx := context.Background()
	layer.SetRaw(ctx, nil, []string{"live-runner"}, in)

	got, ok := layer.GetRaw(ctx, nil, []string{"live-runner"})
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(got) != 1 || got[0].Address != in[0].Address {
		t.Fatalf("got %#v", got)
	}

	got[0].Capabilities[0] = "mutated"
	got2, ok := layer.GetRaw(ctx, nil, []string{"live-runner"})
	if !ok {
		t.Fatal("expected second cache hit")
	}
	if got2[0].Capabilities[0] != "transcode/ffmpeg" {
		t.Fatalf("cached slice was mutated: %#v", got2)
	}
}

func TestRawCacheKeyIgnoresCapOrder(t *testing.T) {
	layer, err := New(time.Minute, "", func() int64 { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	layer.SetRaw(ctx, []string{"b", "a"}, []string{"live-runner"}, []discotypes.WebhookOrchestrator{
		{
			Address: "https://a.example",
			Score:   1,
		},
	})
	got, ok := layer.GetRaw(ctx, []string{"a", "b"}, []string{"live-runner"})
	if !ok || len(got) != 1 {
		t.Fatalf("order-insensitive key failed: ok=%v got=%#v", ok, got)
	}
}

func TestRawCacheInvalidateAll(t *testing.T) {
	layer, err := New(time.Minute, "", func() int64 { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	layer.SetRaw(ctx, nil, []string{"live-runner"}, []discotypes.WebhookOrchestrator{
		{
			Address: "https://a.example",
			Score:   1,
		},
	})
	layer.InvalidateAll()
	if _, ok := layer.GetRaw(ctx, nil, []string{"live-runner"}); ok {
		t.Fatal("expected miss after InvalidateAll")
	}
}

func TestRawCacheExpires(t *testing.T) {
	layer, err := New(time.Millisecond, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	layer.SetRaw(ctx, nil, []string{"live-runner"}, []discotypes.WebhookOrchestrator{
		{
			Address: "https://a.example",
			Score:   1,
		},
	})
	time.Sleep(5 * time.Millisecond)
	if _, ok := layer.GetRaw(ctx, nil, []string{"live-runner"}); ok {
		t.Fatal("expected expired raw cache miss")
	}
}
