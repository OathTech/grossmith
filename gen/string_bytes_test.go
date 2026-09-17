package gen

import (
	"go/ast"
	"go/token"
	"strconv"
	"testing"
	"unicode/utf8"
)

func TestByteStringSlicesRespectTheProfile(t *testing.T) {
	total := 0
	for _, allow := range []bool{true, false} {
		for seed := int64(1); seed <= 100; seed++ {
			cfg := DefaultConfig(seed)
			cfg.Swarm, cfg.Corner = false, "none"
			if !allow {
				cfg.Exclude = []string{"string_bytes"}
			}
			c, err := New(cfg).Generate()
			if err != nil {
				t.Fatal(err)
			}
			_, file, _ := typecheckCase(t, c, seed, nil)
			sites := 0
			ast.Inspect(file, func(n ast.Node) bool {
				slice, ok := n.(*ast.SliceExpr)
				if !ok {
					return true
				}
				literal, ok := slice.X.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				word, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				lo, loConst := slice.Low.(*ast.BasicLit)
				hi, hiConst := slice.High.(*ast.BasicLit)
				if loConst && hiConst {
					a, errA := strconv.Atoi(lo.Value)
					b, errB := strconv.Atoi(hi.Value)
					if errA != nil || errB != nil || a < 0 || a > b || b > len(word) {
						t.Fatal("generated constant string bounds are invalid")
					}
					if !utf8.ValidString(word[a:b]) {
						sites++
					}
				} else if len(runeBounds(word)) != len(word)+1 {
					// A dynamic byte offset can split a multi-byte rune.
					sites++
				}
				return true
			})
			if (sites > 0) != hasFeature(c, "string_bytes") {
				t.Fatalf("seed %d: %d byte-string sites, tag=%v", seed, sites, hasFeature(c, "string_bytes"))
			}
			if !allow && sites != 0 {
				t.Fatalf("seed %d: byte-string slicing escaped its profile exclusion", seed)
			}
			total += sites
		}
	}
	if total == 0 {
		t.Fatal("no byte-string slicing emitted")
	}
	t.Logf("%d byte-string slice sites", total)
}
