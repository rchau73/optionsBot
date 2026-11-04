package tests

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The video manifest coverage test (#611, epic E11): every storyboard has an
// entry in docs/product/education/videos.json, every tip's video id resolves
// and lists the tip, and every caption file is plain words (the rules of the
// tips, #492: sentences of 25 words or fewer, no undefined jargon, no promised
// return). Nothing here records or publishes a video.

const maxVideoSeconds = 90

type video struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Task        string   `json:"task"`
	DurationSec int      `json:"duration_sec"`
	Captions    string   `json:"captions"`
	Tips        []string `json:"tips"`
	Status      string   `json:"status"`
	URL         string   `json:"url"`
}

func loadVideos(t *testing.T) map[string]video {
	t.Helper()
	var videos map[string]video
	if err := json.Unmarshal(readRepoFile(t, "docs/product/education/videos.json"), &videos); err != nil {
		t.Fatal(err)
	}
	return videos
}

// captionText is the spoken text of a WebVTT file: no header, no cue times.
func captionText(vtt string) []string {
	var out []string
	for _, line := range strings.Split(vtt, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "WEBVTT" || strings.Contains(line, "-->") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// videosCoverage returns every problem of the manifest; captions maps a
// captions path to the file's content.
func videosCoverage(videos map[string]video, storyboards map[string]bool, tips map[string]tip, g map[string]glossaryTip, captions map[string]string) []string {
	defined := glossaryIndex(g)
	var out []string
	for id := range storyboards {
		if _, ok := videos[id]; !ok {
			out = append(out, id+": the storyboard has no entry in videos.json")
		}
	}
	for id, v := range videos {
		if !storyboards[id] {
			out = append(out, id+": videos.json entry has no storyboard in video-storyboards.md")
		}
		if v.ID != id {
			out = append(out, id+": id field is \""+v.ID+"\"")
		}
		if strings.TrimSpace(v.Title) == "" {
			out = append(out, id+": no title")
		}
		if strings.TrimSpace(v.Task) == "" {
			out = append(out, id+": no task")
		} else if len(longTipSentences(v.Task)) > 0 || strings.Contains(strings.ToLower(v.Task), " and then ") {
			out = append(out, id+": task is not one short sentence")
		}
		if v.DurationSec <= 0 || v.DurationSec > maxVideoSeconds {
			out = append(out, id+": duration_sec must be 1-90")
		}
		if v.Status != "storyboard" && v.Status != "recorded" {
			out = append(out, id+": status must be storyboard or recorded")
		}
		if v.Status == "storyboard" && v.URL != "" {
			out = append(out, id+": an unrecorded video has no file URL")
		}
		content, ok := captions[v.Captions]
		if v.Captions == "" || !ok {
			out = append(out, id+": captions file \""+v.Captions+"\" not found")
		}
		for _, line := range captionText(content) {
			out = append(out, tipProblems(id+" captions", line, id, defined)...)
		}
		for _, key := range v.Tips {
			if tips[key].Video != id {
				out = append(out, id+": lists tip "+key+", which does not name this video")
			}
		}
	}
	for key, tp := range tips {
		if tp.Video == "" {
			continue
		}
		v, ok := videos[tp.Video]
		if !ok {
			out = append(out, key+": video "+tp.Video+" is not in videos.json")
			continue
		}
		listed := false
		for _, k := range v.Tips {
			listed = listed || k == key
		}
		if !listed {
			out = append(out, key+": "+tp.Video+" does not list this tip in its tips field")
		}
	}
	sort.Strings(out)
	return out
}

func readCaptions(t *testing.T, videos map[string]video) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, v := range videos {
		if !regexp.MustCompile(`^docs/product/education/captions/video\.\w+\.vtt$`).MatchString(v.Captions) {
			continue
		}
		files[v.Captions] = string(readRepoFile(t, v.Captions))
	}
	return files
}

// Scenarios: every storyboard has a manifest entry; every tip's video id
// resolves; captions are plain.
func TestVideos_ManifestCoversStoryboardsTipsAndCaptions(t *testing.T) {
	videos := loadVideos(t)
	problems := videosCoverage(videos, storyboardVideos(t), loadTips(t), loadGlossaryTips(t), readCaptions(t, videos))
	if len(problems) > 0 {
		t.Errorf("video manifest problems:\n%s", strings.Join(problems, "\n"))
	}
}

func TestVideos_FileIsInAlphabeticalOrder(t *testing.T) {
	raw := string(readRepoFile(t, "docs/product/education/videos.json"))
	var ids []string
	for _, m := range regexp.MustCompile(`(?m)^  "(video\.\w+)": \{`).FindAllStringSubmatch(raw, -1) {
		ids = append(ids, m[1])
	}
	if len(ids) == 0 || !sort.StringsAreSorted(ids) {
		t.Errorf("videos.json keys must stay in alphabetical order: %v", ids)
	}
	var fields []string
	for _, m := range regexp.MustCompile(`(?m)^    "(\w+)": `).FindAllStringSubmatch(raw, -1) {
		fields = append(fields, m[1])
	}
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] != "title" && fields[i] > fields[i+1] { // "title" ends an entry
			t.Errorf("videos.json fields must be alphabetical: %q before %q", fields[i], fields[i+1])
		}
	}
}

func TestVideos_CoverageFailuresNameTheCulprit(t *testing.T) {
	g := map[string]glossaryTip{"delta": {Short: "How far.", RiskIncome: "More."}}
	good := video{ID: "video.a", Title: "A", Task: "Do one thing.", DurationSec: 60, Captions: "c.vtt", Tips: []string{"k"}, Status: "storyboard"}
	vtt := "WEBVTT\n\n00:00:00.000 --> 00:00:05.000\nA plain line.\n"
	run := func(mod func(*video), caption string, tipVideo string, storyboards map[string]bool) string {
		v := good
		mod(&v)
		return strings.Join(videosCoverage(map[string]video{"video.a": v}, storyboards, map[string]tip{"k": {Video: tipVideo}}, g, map[string]string{"c.vtt": caption}), "\n")
	}
	sb := map[string]bool{"video.a": true}
	if got := run(func(*video) {}, vtt, "video.a", sb); got != "" {
		t.Fatalf("a clean manifest must pass, got %s", got)
	}
	cases := []struct {
		name, got, want string
	}{
		{"storyboard without entry", run(func(*video) {}, vtt, "video.a", map[string]bool{"video.a": true, "video.b": true}), "video.b: the storyboard has no entry"},
		{"entry without storyboard", run(func(*video) {}, vtt, "video.a", map[string]bool{}), "video.a: videos.json entry has no storyboard"},
		{"over 90 s", run(func(v *video) { v.DurationSec = 91 }, vtt, "video.a", sb), "duration_sec must be 1-90"},
		{"no title", run(func(v *video) { v.Title = " " }, vtt, "video.a", sb), "no title"},
		{"no captions path", run(func(v *video) { v.Captions = "" }, vtt, "video.a", sb), "captions file"},
		{"url while unrecorded", run(func(v *video) { v.URL = "https://x/y.mp4" }, vtt, "video.a", sb), "no file URL"},
		{"tip names an unknown video", run(func(*video) {}, vtt, "video.zzz", sb), "k: video video.zzz is not in videos.json"},
		{"tip not listed", run(func(v *video) { v.Tips = nil }, vtt, "video.a", sb), "does not list this tip"},
		{"long caption sentence", run(func(*video) {}, "WEBVTT\n\n00:00:00.000 --> 00:00:05.000\n"+strings.Repeat("word ", 26)+"end.\n", "video.a", sb), "sentence over 25 words"},
		{"undefined jargon", run(func(*video) {}, "WEBVTT\n\n00:00:00.000 --> 00:00:05.000\nWatch the vega.\n", "video.a", sb), "uses \"vega\""},
		{"promised return", run(func(*video) {}, "WEBVTT\n\n00:00:00.000 --> 00:00:05.000\nIt is risk-free.\n", "video.a", sb), "promises or implies a return"},
	}
	for _, c := range cases {
		if !strings.Contains(c.got, c.want) {
			t.Errorf("%s: want a problem containing %q, got:\n%s", c.name, c.want, c.got)
		}
	}
}
