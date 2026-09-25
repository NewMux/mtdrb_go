package sync

import "testing"

func TestCursorRoundTripsAndResetsNamedCollections(t *testing.T) {
	c := Cursor{"clients": 40, "packages": 12, "sessions": 7}
	decoded, err := DecodeCursor(c.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if decoded["clients"] != 40 || decoded["packages"] != 12 {
		t.Fatalf("cursor did not round-trip: %v", decoded)
	}

	// A device whose schema gained a column on clients asks for them again.
	// Blank and unknown names are ignored rather than refused.
	decoded.Reset([]string{"clients", " ", "", "no_such_collection"})
	if _, ok := decoded["clients"]; ok {
		t.Error("clients was not reset")
	}
	if decoded["packages"] != 12 || decoded["sessions"] != 7 {
		t.Errorf("reset touched collections it was not asked to: %v", decoded)
	}

	// An empty cursor, as a first sync sends, takes a reset without panicking.
	empty, err := DecodeCursor("")
	if err != nil {
		t.Fatal(err)
	}
	empty.Reset([]string{"clients"})
}
