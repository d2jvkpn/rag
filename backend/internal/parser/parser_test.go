package parser

import "testing"

func TestCleanText(t *testing.T) {
	cleaned := CleanText("a\r\n\r\n\r\nb\r\n")
	if cleaned != "a\n\nb" {
		t.Fatalf("unexpected cleaned text: %q", cleaned)
	}
}
