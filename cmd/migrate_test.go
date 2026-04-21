package cmd

import (
	"reflect"
	"testing"

	cf "github.com/essentialkaos/go-confluence/v6"
)

func TestUpdateUrlMap(t *testing.T) {
	m := map[string]UrlMapEntry{}
	m = updateUrlMap(m, "/display/ENG/Home", "/doc/home-abc", "doc-1")
	m = updateUrlMap(m, "/pages/viewpage.action?pageId=42", "/doc/home-abc", "doc-1")
	m = updateUrlMap(m, "/display/ENG/Home", "/doc/home-xyz", "doc-2") // overwrites

	want := map[string]UrlMapEntry{
		"/display/ENG/Home":                {NewUrl: "/doc/home-xyz", DocId: "doc-2"},
		"/pages/viewpage.action?pageId=42": {NewUrl: "/doc/home-abc", DocId: "doc-1"},
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("updateUrlMap = %v, want %v", m, want)
	}
}

func TestRewriteConfluenceURL(t *testing.T) {
	entry := UrlMapEntry{NewUrl: "/doc/home-abc", DocId: "doc-1"}
	const confluence = "https://confluence.example.com"
	const outline = "https://outline.example.com"

	tests := []struct {
		name   string
		oldUrl string
		body   string
		want   string
	}{
		{
			name:   "relative link rewrites to relative",
			oldUrl: "/display/ENG/Home",
			body:   "See [Home](/display/ENG/Home) for details.",
			want:   "See [Home](/doc/home-abc) for details.",
		},
		{
			name:   "absolute link rewrites to absolute",
			oldUrl: "/display/ENG/Home",
			body:   "See [Home](https://confluence.example.com/display/ENG/Home).",
			want:   "See [Home](https://outline.example.com/doc/home-abc).",
		},
		{
			name:   "text match outside parens is left alone",
			oldUrl: "/display/ENG/Home",
			body:   "The path /display/ENG/Home appears in prose.",
			want:   "The path /display/ENG/Home appears in prose.",
		},
		{
			name:   "no match leaves body untouched",
			oldUrl: "/display/ENG/Missing",
			body:   "nothing here",
			want:   "nothing here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteConfluenceURL(tt.oldUrl, entry, tt.body, confluence, outline)
			if got != tt.want {
				t.Errorf("rewriteConfluenceURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBodyHasBrokenLink(t *testing.T) {
	entry := UrlMapEntry{NewUrl: "/doc/home-abc"}
	const outline = "https://outline.example.com"

	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "relative broken link shape matches",
			body: "prefix\n](/doc/home-abc)[suffix",
			want: true,
		},
		{
			name: "absolute broken link shape matches",
			body: "prefix\n](https://outline.example.com/doc/home-abc)[suffix",
			want: true,
		},
		{
			name: "well-formed link does not match",
			body: "See [Home](/doc/home-abc) here.",
			want: false,
		},
		{
			name: "empty body",
			body: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bodyHasBrokenLink(tt.body, entry, outline); got != tt.want {
				t.Errorf("bodyHasBrokenLink() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPossibleConfluenceURLs(t *testing.T) {
	m := Migrator{spaceKey: "ENG"}
	page := &cf.Content{ID: "42", Title: "Release Notes: v1.0"}

	got := m.getPossibleConfluenceURLs(page)
	want := []string{
		"/pages/viewpage.action?pageId=42",
		"/display/ENG/Release Notes%3A v1.0",
		"/display/ENG/Release+Notes%3A+v1.0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("getPossibleConfluenceURLs() = %v, want %v", got, want)
	}
}

func TestAddToMarkedListDeduplicates(t *testing.T) {
	m := Migrator{
		urlMap: map[string]UrlMapEntry{
			"/display/ENG/Home": {NewUrl: "/doc/home-abc", DocId: "doc-1"},
		},
	}
	doc := DocumentData{DocId: "doc-1"}

	var marked []JsonOutputVars
	marked = m.addToMarkedList(doc, marked)
	marked = m.addToMarkedList(doc, marked) // second call must not duplicate

	if len(marked) != 1 {
		t.Fatalf("expected 1 marked entry, got %d: %+v", len(marked), marked)
	}
	want := JsonOutputVars{Id: "doc-1", OutUrl: "/doc/home-abc", ConfURL: "/display/ENG/Home"}
	if marked[0] != want {
		t.Errorf("marked[0] = %+v, want %+v", marked[0], want)
	}
}

func TestAddToMarkedListIgnoresUnknownDoc(t *testing.T) {
	m := Migrator{
		urlMap: map[string]UrlMapEntry{
			"/display/ENG/Home": {NewUrl: "/doc/home-abc", DocId: "doc-1"},
		},
	}
	doc := DocumentData{DocId: "doc-unknown"}
	marked := m.addToMarkedList(doc, nil)
	if marked != nil {
		t.Errorf("expected nil, got %+v", marked)
	}
}
