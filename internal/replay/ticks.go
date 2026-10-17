package replay

import (
	"sort"
	"time"

	"optionsbot/internal/recorder"
)

// A tick is one decision of the replay: an hourly book alone, or a minute
// GEX record with the last book before it (Settings.GEX).
type tick struct {
	at   time.Time
	book int                 // index of the book in snaps
	gex  *recorder.GEXRecord // nil: GEX rebuilt from the book
}

// gexCover: a book with a minute record this close does not decide on its
// own — the records around it do, on its prices.
const gexCover = 2 * time.Minute

// maxBookAge: a minute record more than this after the last book is not
// replayed (the hourly recording stopped; there are no prices to trade on).
const maxBookAge = 2 * time.Hour

// ticks merges the books and the minute records into decisions, in time
// order. A book and a record at the same time: the book comes first, so the
// record decides on the fresh prices.
func ticks(snaps []recorder.Snapshot, recs []recorder.GEXRecord) []tick {
	covered := func(at time.Time) bool {
		i := sort.Search(len(recs), func(i int) bool { return !recs[i].ComputedAt.Before(at.Add(-gexCover)) })
		return i < len(recs) && !recs[i].ComputedAt.After(at.Add(gexCover))
	}
	var out []tick
	book, j := -1, 0
	for i := 0; i <= len(snaps); i++ {
		// the records before book i (all the rest after the last book)
		for ; j < len(recs) && (i == len(snaps) || recs[j].ComputedAt.Before(snaps[i].At)); j++ {
			if book >= 0 && recs[j].ComputedAt.Sub(snaps[book].At) <= maxBookAge {
				out = append(out, tick{at: recs[j].ComputedAt, book: book, gex: &recs[j]})
			}
		}
		if i == len(snaps) {
			break
		}
		book = i
		if !covered(snaps[i].At) {
			out = append(out, tick{at: snaps[i].At, book: i})
		}
	}
	return out
}
