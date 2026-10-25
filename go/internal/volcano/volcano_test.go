package volcano

import (
	"os"
	"strings"
	"testing"
)

func load(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/wovodat-list.html")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseLevels(t *testing.T) {
	st, err := Parse(load(t))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"Taal": 1, "Kanlaon": 2, "Bulusan": 1, "Pinatubo": 0, "Mayon": 2}
	for k, v := range want {
		if st.Levels[k] != v {
			t.Errorf("level %s = %d, want %d (all=%v)", k, st.Levels[k], v, st.Levels)
		}
	}
	if st.Taal.Level != 1 {
		t.Errorf("Taal level = %d, want 1", st.Taal.Level)
	}
	if st.Taal.LevelText != "Low-level unrest" {
		t.Errorf("Taal text = %q", st.Taal.LevelText)
	}
}

func TestParseBulletin(t *testing.T) {
	st, err := Parse(load(t))
	if err != nil {
		t.Fatal(err)
	}
	if st.Taal.BulletinID != "16933" {
		t.Errorf("bid = %q, want 16933", st.Taal.BulletinID)
	}
	if !strings.Contains(st.Taal.BulletinURL, "activity-tvo?bid=16933") {
		t.Errorf("url = %q", st.Taal.BulletinURL)
	}
	if !strings.HasPrefix(st.Taal.BulletinURL, "https://") {
		t.Errorf("bulletin URL should be absolute, got %q", st.Taal.BulletinURL)
	}
	wantTitle := "Taal Volcano Summary of 24Hr Observation 25 October 2026 12:00 AM"
	if st.Taal.BulletinTitle != wantTitle {
		t.Errorf("title = %q, want %q", st.Taal.BulletinTitle, wantTitle)
	}
}

func TestLevelDescriptions(t *testing.T) {
	cases := map[int]string{
		0: "Normal",
		1: "Low-level unrest",
		2: "Increasing unrest",
		3: "Magmatic unrest",
		4: "Hazardous eruption imminent",
		5: "Hazardous eruption in progress",
	}
	for lvl, want := range cases {
		if got := LevelDescription(lvl); got != want {
			t.Errorf("LevelDescription(%d) = %q, want %q", lvl, got, want)
		}
	}
	if LevelDescription(9) != "Unknown" {
		t.Error("unknown level should be Unknown")
	}
}

func TestParseNoLevels(t *testing.T) {
	if _, err := Parse([]byte("<html><body>nothing here</body></html>")); err == nil {
		t.Error("expected error when no levels found")
	}
}

func TestLatestBidWins(t *testing.T) {
	html := `<html><body>
	Alert Level Status: Taal - 2 Kanlaon - 1
	<a href="/bulletin/activity-tvo?bid=100"><p>Taal Volcano Summary Old</p></a>
	<a href="/bulletin/activity-tvo?bid=200"><p>Taal Volcano Summary New 25 October 2026</p></a>
	</body></html>`
	st, err := Parse([]byte(html))
	if err != nil {
		t.Fatal(err)
	}
	if st.Taal.BulletinID != "200" {
		t.Errorf("bid = %q, want 200 (max wins)", st.Taal.BulletinID)
	}
}
