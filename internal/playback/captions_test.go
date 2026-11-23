package playback

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSavedWebVTTKeepsTextAndOffsets(t *testing.T) {
	before, err := ReadCaptions(strings.NewReader("00:00:00.500 --> 00:00:02.200\nFirst &amp; &lt;second&gt;\nLine two\n\n" +
		"00:00:01.000 --> 00:00:03.000\nOverlap\n"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := before.WebVTT(-750 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	after, err := ReadCaptions(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if text := after.At(500 * time.Millisecond); text != "First & <second>\nLine two\nOverlap" {
		t.Fatalf("saved captions changed rendered text: %q", text)
	}
	if text := after.At(1500 * time.Millisecond); text != "Overlap" {
		t.Fatalf("saved delay did not shift cue boundaries: %q", text)
	}
}

func TestReadCaptionsHandlesOverlapsAndSeeks(t *testing.T) {
	captions, err := ReadCaptions(strings.NewReader("\uFEFFWEBVTT\r\n\r\nNOTE ignore this block\r\n\r\n" +
		"later\r\n00:04.000 --> 00:05.000 align:center\r\nSecond &amp; third\r\n\r\n" +
		"1\r\n00:00:01,500 --> 00:00:06,000\r\n<i>First</i>\r\nLine two\r\n\r\n" +
		"00:00:11.000 --> 00:00:08.000\r\nMalformed"))
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range []struct {
		at   time.Duration
		want string
	}{
		{4 * time.Second, "First\nLine two\nSecond & third"},
		{6 * time.Second, ""},
		{1500 * time.Millisecond, "First\nLine two"},
		{1499 * time.Millisecond, ""},
		{11 * time.Second, ""},
	} {
		if text := captions.At(example.at); text != example.want {
			t.Errorf("captions at %v = %q, want %q", example.at, text, example.want)
		}
	}
}

func TestFetchCaptionsKeepsRequestsOnTheMediaServer(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("followed a caption redirect to another origin")
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/caption":
			http.Redirect(w, r, "/text?credential=fixture", http.StatusFound)
		case "/text":
			if r.URL.Query().Get("credential") != "fixture" {
				t.Error("did not send the required query credential")
			}
			io.WriteString(w, "WEBVTT\n\n00:01.000 --> 00:03.000\nAuthenticated caption")
		case "/redirect":
			http.Redirect(w, r, destination.URL, http.StatusFound)
		}
	}))
	defer source.Close()
	track, err := FetchCaptions(t.Context(), source.URL+"/caption?credential=fixture", nil)
	if err != nil || track.At(time.Second) != "Authenticated caption" {
		t.Fatalf("fetch caption from the original server: %v", err)
	}
	if _, err := FetchCaptions(t.Context(), source.URL+"/redirect?credential=fixture", nil); err == nil ||
		strings.Contains(err.Error(), "fixture") {
		t.Fatal("redirect did not return a credential-free error")
	}
}
