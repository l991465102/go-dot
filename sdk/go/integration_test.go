package godot

import (
	"context"
	"os"
	"testing"
)

// TestLiveTestEnvironment 验证 Go SDK 对真实测试采集接口的请求与响应。
func TestLiveTestEnvironment(t *testing.T) {
	base, key, code := os.Getenv("GO_DOT_SDK_TEST_URL"), os.Getenv("GO_DOT_SDK_TEST_KEY"), os.Getenv("GO_DOT_SDK_TEST_POINT")
	if base == "" || key == "" || code == "" {
		t.Skip("requires an isolated collection fixture")
	}
	c, err := NewClient(Options{BaseURL: base, WriteKey: key, Test: true, MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	events := []Event{{PointCode: code, AnonymousID: "sdk-live-test", Platform: "web", Properties: map[string]any{}}}
	got, err := c.Send(context.Background(), events)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != "accepted" || got[0].EventID != events[0].EventID {
		t.Fatalf("unexpected results: %+v", got)
	}
}
