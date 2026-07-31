package web

import "testing"

// The handler joins this name onto a directory, so it decides whether the
// endpoint serves screenshots or any file the process can read.
func TestValidShotName(t *testing.T) {
	valid := []string{"glance.png", "lokal-docker-searxng.png", "a.png", "x1-2-3.png"}
	for _, n := range valid {
		if !validShotName(n) {
			t.Errorf("%q should be valid", n)
		}
	}

	invalid := []string{
		"",
		".png",                   // no slug
		"glance.PNG",             // extension is exact
		"glance.jpg",             // only png is served
		"glance",                 // no extension
		"../../.env.png",         // traversal
		"..%2F..%2Fetc%2Fpasswd", // encoded traversal
		"/etc/passwd.png",        // absolute
		"sub/dir.png",            // nested
		"-glance.png",            // leading dash
		"glance-.png",            // trailing dash
		"Glance.png",             // uppercase never comes out of slugify
		"glance .png",            // space
		"glance$.png",            // punctuation
		"glance.png.png",         // double extension resolves to a slug with a dot
		"gläance.png",            // non-ascii
	}
	for _, n := range invalid {
		if validShotName(n) {
			t.Errorf("%q should be rejected", n)
		}
	}
}
