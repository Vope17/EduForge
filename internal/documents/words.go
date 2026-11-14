package documents

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"
	"sort"
	"strings"
	"sync"
)

// words.txt.gz holds the lowercase alphabetic words of the public-domain
// Fedora 'words' package (/usr/share/dict/words, Moby Words II), sorted.
// It confirms ligature repairs and line-break hyphen joins.
//
//go:embed words.txt.gz
var wordListGz []byte

var (
	wordsOnce sync.Once
	words     []string
)

func loadWords() {
	r, err := gzip.NewReader(bytes.NewReader(wordListGz))
	if err != nil {
		return
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return
	}
	words = strings.Fields(string(b))
	sort.Strings(words)
}

// knownWord reports whether w (any case) is an English word.
func knownWord(w string) bool {
	wordsOnce.Do(loadWords)
	w = strings.ToLower(w)
	i := sort.SearchStrings(words, w)
	return i < len(words) && words[i] == w
}
