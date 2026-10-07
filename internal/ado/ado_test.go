package ado

import (
	"encoding/json"
	"testing"
)

func TestDeletedChangeKeepsItsPath(t *testing.T) {
	// A deletion as /iterations/{id}/changes returns it.
	raw := `{"changeTrackingId":101,"originalPath":"/WebConsole/src/utilities/hooks/historyPathHook.ts",
		"item":{"originalObjectId":"5E71AA4F","path":null},"changeType":"delete"}`
	var ch Change
	if err := json.Unmarshal([]byte(raw), &ch); err != nil {
		t.Fatal(err)
	}
	if got := ch.withPath().Item.Path; got != "/WebConsole/src/utilities/hooks/historyPathHook.ts" {
		t.Fatalf("a deleted file should keep its path, got %q", got)
	}
}
