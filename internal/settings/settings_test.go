package settings

import "testing"

func TestWorkingHoursAreSortedAndChecked(t *testing.T) {
	got, err := WorkingHours{
		"1": {{"16:00", "20:00"}, {"06:00", "12:00"}},
		"5": {},
	}.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if got["1"][0][0] != "06:00" || len(got) != 1 {
		t.Fatalf("expected Monday sorted and the empty Friday dropped, got %v", got)
	}

	for name, bad := range map[string]WorkingHours{
		"unknown day": {"7": {{"06:00", "07:00"}}},
		"backwards":   {"1": {{"09:00", "08:00"}}},
		"not a time":  {"1": {{"6am", "07:00"}}},
		"overlapping": {"1": {{"06:00", "10:00"}, {"09:00", "11:00"}}},
		"hour 24":     {"1": {{"23:00", "24:00"}}},
	} {
		if _, err := bad.Validate(); err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
	}
}

func TestTheWeekFollowsTheCountry(t *testing.T) {
	for country, want := range map[string]int{"AE": 1, "SA": 0, "sa": 0, "OM": 0, "GB": 1} {
		if got := DefaultWeekStart(country); got != want {
			t.Errorf("%s: week starts on %d, want %d", country, got, want)
		}
	}
}
