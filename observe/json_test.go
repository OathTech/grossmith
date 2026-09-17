package observe

import (
	"strings"
	"testing"
)

func TestParseRejectsAmbiguousJSON(t *testing.T) {
	good := `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"int","goType":"int","int":1}]}`
	for name, raw := range map[string]string{
		"duplicate status": strings.Replace(good, `"status":`, `"status":"error","status":`, 1),
		"status alias":     strings.Replace(good, `"status":"ok"`, `"status":"error","Status":"ok"`, 1),
		"value alias":      strings.Replace(good, `"int":1`, `"int":999,"Int":1`, 1),
		"Unicode alias":    strings.Replace(good, `"status":"ok"`, `"status":"error","ſtatus":"ok"`, 1),
		"duplicate value":  strings.Replace(good, `"int":1`, `"int":0,"int":1`, 1),
		"escaped name":     strings.Replace(good, `"int":1`, `"int":0,"\u0069nt":1`, 1),
		"closing bracket":  good + "]",
		"closing brace":    good + "}",
		"second document":  good + "{}",
		"invalid UTF-8":    `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"string","goType":"string","str":"` + "\xff" + `"}]}`,
		"unpaired escape":  `{"schema":"grossmith-observation-v2","status":"ok","values":[{"kind":"string","goType":"string","str":"\ud800"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(raw)); err == nil {
				t.Fatal("ambiguous or malformed observation was accepted")
			}
		})
	}
}
