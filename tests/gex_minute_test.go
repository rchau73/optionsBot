package tests

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
)

// #122: every GEX snapshot recorded (one a minute) so the replay decides the
// GEX side at the bot's pace.

type fakeGEX struct{ snap *gex.Snapshot }

func (f *fakeGEX) Snapshot() *gex.Snapshot { return f.snap }

// Scenario (writer): each new snapshot is written once, with its median flip
// over the bias window, the DVOL and the shed buffer, into its UTC day's file.
func TestGEXRecorder_WritesEachSnapshotOnce(t *testing.T) {
	dir := t.TempDir()
	src := &fakeGEX{}
	rec := recorder.NewGEXRecorder(src, func() float64 { return 48 }, func() float64 { return 3.8 }, time.Hour, dir)
	if wrote, err := rec.Record(); wrote || err != nil {
		t.Fatalf("no snapshot yet: wrote %v err %v, want nothing", wrote, err)
	}
	start := time.Date(2026, 10, 17, 23, 58, 0, 0, time.UTC)
	flips := []float64{70000, 72000, 90000} // the last one a jump
	for i, f := range flips {
		src.snap = &gex.Snapshot{ComputedAt: start.Add(time.Duration(i) * time.Minute), Spot: 75000, Regime: "NEGATIVE/ACCELERATION",
			GammaFlip: f, GammaFlipFound: true, RegimeScore: -1.5e6}
		if wrote, err := rec.Record(); !wrote || err != nil {
			t.Fatalf("snapshot %d: wrote %v err %v", i, wrote, err)
		}
		if wrote, _ := rec.Record(); wrote {
			t.Fatalf("snapshot %d written twice", i)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-10-18.jsonl.gz")); err != nil {
		t.Errorf("the 00:00 snapshot goes to the next day's file: %v", err)
	}
	recs, err := recorder.ReadGEXDir(dir, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("records = %d, want 3", len(recs))
	}
	r := recs[2]
	want := recorder.GEXRecord{ComputedAt: start.Add(2 * time.Minute), Spot: 75000, Regime: "NEGATIVE/ACCELERATION", Flip: 90000,
		FlipFound: true, MedianFlip: 72000, GEXSum: -1.5e6, DVOL: 48, FlipBufferPct: 3.8}
	if r != want {
		t.Errorf("record = %+v\nwant     %+v", r, want)
	}
	if recs[1].MedianFlip != 71000 {
		t.Errorf("median of two flips = %v, want 71000", recs[1].MedianFlip)
	}
	if s := r.Snapshot(); s.GammaFlip != 90000 || s.Regime != r.Regime || !s.ComputedAt.Equal(r.ComputedAt) || s.RegimeScore != r.GEXSum {
		t.Errorf("snapshot = %+v, want the record's fields", s)
	}
	day2, _ := recorder.ReadGEXDir(dir, time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC), time.Time{})
	if len(day2) != 1 {
		t.Errorf("from 10-18: %d records, want 1", len(day2))
	}
}

// Scenario (reader): a torn last member is dropped, the records before it
// are read; a folder without records is no records, not an error.
func TestGEXRecorder_ReadTornAndMissing(t *testing.T) {
	dir := t.TempDir()
	src := &fakeGEX{}
	rec := recorder.NewGEXRecorder(src, nil, nil, 0, dir)
	at := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		src.snap = &gex.Snapshot{ComputedAt: at.Add(time.Duration(i) * time.Minute), Spot: 75000, Regime: "NEUTRAL"}
		if _, err := rec.Record(); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "2026-10-17.jsonl.gz")
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write([]byte(`{"computed_at":"2026-10-17T10:02:00Z","spot":75000,"regime":"NEUTRAL","flip":0}` + "\n"))
	zw.Close()
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.Write(buf.Bytes()[:buf.Len()/2]) // cut inside the data
	f.Close()

	recs, err := recorder.ReadGEXFile(path)
	if err != nil {
		t.Fatalf("a torn last record must not fail the read: %v", err)
	}
	if len(recs) != 2 || recs[0].MedianFlip != 0 {
		t.Errorf("records = %+v, want the 2 complete ones (no median without a window)", recs)
	}
	none, err := recorder.ReadGEXDir(filepath.Join(dir, "absent"), time.Time{}, time.Time{})
	if err != nil || len(none) != 0 {
		t.Errorf("missing folder: %d records, err %v; want none, no error", len(none), err)
	}
}

// minuteGEX is a record each minute from `from` for n minutes; negative(m)
// says whether minute m is in a negative regime with the flip 10 % under
// spot (bias up: the calls are shed), else the regime is positive.
func minuteGEX(from time.Time, n int, every time.Duration, negative func(at time.Time) bool) []recorder.GEXRecord {
	var out []recorder.GEXRecord
	for m := 0; m < n; m++ {
		at := from.Add(time.Duration(m) * every)
		r := recorder.GEXRecord{ComputedAt: at, Spot: 100000, Regime: "POSITIVE/PINNING", Flip: 90000, FlipFound: true,
			MedianFlip: 90000, GEXSum: 1e6, DVOL: 50}
		if negative(at) {
			r.Regime, r.GEXSum = "NEGATIVE/ACCELERATION", -1e6
		}
		out = append(out, r)
	}
	return out
}

// Scenario (replay): on minute records the GEX side decides each minute on
// the last hourly book. A 20-minute negative stretch between two books sheds
// the calls under the bias; the same records sampled at the books' hours —
// the hourly pace — never see it.
func TestMarketReplay_MinuteGEXSeesWhatHourlyMisses(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	record(t, dir, start, 4*24, func(int) float64 { return 100000 })
	snaps, _ := recorder.ReadDir(dir, time.Time{}, time.Time{})
	closes, dvol := flatHistory(start)
	from := start.AddDate(0, 0, 3)
	stretch := func(at time.Time) bool {
		return !at.Before(from.Add(12*time.Hour+10*time.Minute)) && at.Before(from.Add(12*time.Hour+30*time.Minute))
	}
	run := func(recs []recorder.GEXRecord) replay.Result {
		t.Helper()
		res, err := replay.Run(snaps, replay.Settings{Cfg: replayConfig(t, "bias"), From: from, Equity: 1,
			Closes: closes, DVOL: dvol, GEX: recs})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	minute := run(minuteGEX(from.Add(-2*time.Hour), 26*60, time.Minute, stretch))
	hourly := run(minuteGEX(from.Add(-2*time.Hour), 26, time.Hour, stretch))

	if minute.Entries != 2 || hourly.Entries != 2 {
		t.Fatalf("entries minute %d hourly %d, want both slots opened", minute.Entries, hourly.Entries)
	}
	if minute.Sheds == 0 {
		t.Errorf("minute pace: no shed, want the calls shed in the 20-minute negative stretch")
	}
	if hourly.Sheds != 0 {
		t.Errorf("hourly pace: %d sheds, want none (the stretch is between two books)", hourly.Sheds)
	}
	if minute.GEXMinutes != 24*60 || minute.Snapshots != 24*60 {
		t.Errorf("minute: %d decisions, %d on records; want one per minute of the day (the books covered)", minute.Snapshots, minute.GEXMinutes)
	}
}

// Scenario (fallback): where the minute records stop, each hourly book
// decides on its own again with its rebuilt GEX; without any records the
// replay is the hourly one.
func TestMarketReplay_FallsBackToHourlyWithoutMinuteGEX(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	record(t, dir, start, 4*24, func(int) float64 { return 100000 })
	snaps, _ := recorder.ReadDir(dir, time.Time{}, time.Time{})
	closes, dvol := flatHistory(start)
	from := start.AddDate(0, 0, 3)
	recs := minuteGEX(from, 13*60, time.Minute, func(time.Time) bool { return false }) // 00:00–12:59
	res, err := replay.Run(snaps, replay.Settings{Cfg: replayConfig(t, "sma"), From: from, Equity: 1, Closes: closes, DVOL: dvol, GEX: recs})
	if err != nil {
		t.Fatal(err)
	}
	// 13:00 is covered by the 12:59 record; 14:00…23:00 decide alone.
	if res.GEXMinutes != 13*60 || res.Snapshots != 13*60+10 {
		t.Errorf("decisions %d (on records %d), want %d records + 10 books", res.Snapshots, res.GEXMinutes, 13*60)
	}
	hourly, err := replay.Run(snaps, replay.Settings{Cfg: replayConfig(t, "sma"), From: from, Equity: 1, Closes: closes, DVOL: dvol})
	if err != nil {
		t.Fatal(err)
	}
	if hourly.GEXMinutes != 0 || hourly.Snapshots != 24 {
		t.Errorf("no records: %d decisions (%d on records), want the 24 books", hourly.Snapshots, hourly.GEXMinutes)
	}
}
