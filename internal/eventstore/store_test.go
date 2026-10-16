package eventstore

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestMetadataOnlyStoreCompactsDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	store, err := New(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 8; index++ {
		_, err = store.Append("node-1", TelemetryMetadata{EventID: "evt", EventHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Service: "smtp", ParserID: "mail", Route: "normal", AttemptCount: index + 1, FieldCount: 4, Result: "success"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := len(store.Query(Query{Limit: 100})); got != 3 {
		t.Fatalf("records=%d", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(data, []byte("\n")) != 3 {
		t.Fatalf("disk was not compacted: %s", data)
	}
	for _, forbidden := range [][]byte{[]byte(`"raw"`), []byte(`"event"`), []byte(`"data"`)} {
		if bytes.Contains(data, forbidden) {
			t.Fatalf("forbidden retained field %s", forbidden)
		}
	}
}
