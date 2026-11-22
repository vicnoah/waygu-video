package playback

import (
	"bufio"
	"compress/gzip"
	"embed"
	"strings"
	"sync"
	"unicode/utf8"
)

//go:embed opencc/*.txt.gz
var chineseData embed.FS

var (
	toTraditional = sync.OnceValue(func() chineseDictionary { return readChineseDictionary("ST") })
	toSimplified  = sync.OnceValue(func() chineseDictionary { return readChineseDictionary("TS") })
)

type chineseDictionary struct {
	words   map[string]string
	longest int
}

func readChineseDictionary(direction string) chineseDictionary {
	dict := chineseDictionary{words: make(map[string]string)}
	for _, kind := range []string{"Characters", "Phrases"} {
		file, err := chineseData.Open("opencc/" + direction + kind + ".txt.gz")
		if err != nil {
			return chineseDictionary{}
		}
		archive, err := gzip.NewReader(file)
		if err != nil {
			file.Close()
			return chineseDictionary{}
		}
		lines := bufio.NewScanner(archive)
		for lines.Scan() {
			line := lines.Text()
			if key, choices, ok := strings.Cut(line, "\t"); ok && !strings.HasPrefix(key, "#") {
				preferred, _, _ := strings.Cut(choices, " ")
				dict.words[key] = preferred
				dict.longest = max(dict.longest, len(key))
			}
		}
		archive.Close()
		file.Close()
		if lines.Err() != nil {
			return chineseDictionary{}
		}
	}
	return dict
}

// ConvertChinese uses phrase matches before individual character forms. target
// is the preferred Chinese script, zh-Hans or zh-Hant.
func ConvertChinese(text, target string) string {
	var dict chineseDictionary
	switch target {
	case "zh-Hans":
		dict = toSimplified()
	case "zh-Hant":
		dict = toTraditional()
	default:
		return text
	}
	var converted strings.Builder
	converted.Grow(len(text))
	for len(text) > 0 {
		_, size := utf8.DecodeRuneInString(text)
		replacement, length := text[:size], size
		if size > 1 {
			for end := min(len(text), dict.longest); end >= size; end-- {
				if value, ok := dict.words[text[:end]]; ok {
					replacement, length = value, end
					break
				}
			}
		}
		converted.WriteString(replacement)
		text = text[length:]
	}
	return converted.String()
}
