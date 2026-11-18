package steps

import (
	"reflect"
	"testing"
)

func TestParsePipOutdated(t *testing.T) {
	out := `[{"name": "markitdown", "version": "0.0.2", "latest_version": "0.1.7", "latest_filetype": "wheel"}, {"name": "pip", "version": "26.2.2", "latest_version": "26.3", "latest_filetype": "wheel"}]`
	got, err := parsePipOutdated(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []pipOutdated{{"markitdown", "0.0.2", "0.1.7"}, {"pip", "26.2.2", "26.3"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if _, err := parsePipOutdated("not json"); err == nil {
		t.Error("expected an error for non-JSON output")
	}
}
