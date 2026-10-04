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

// TestByteStringsRealisedWhenEnabled pins a floor on how often a case whose
// mix enables string_bytes, and which reaches a string slice site, actually
// emits a UTF-8-splitting slice. Before the utf8-split arm the construct was
// realised in 5 of 1000 default-profile cases (seeds 1-1000) with ~52
// eligible; the arm brings it to 30 of 52. The floor is loose: generation is
// deterministic, so this guards against the arm being masked or starved, not
// against noise.
func TestByteStringsRealisedWhenEnabled(t *testing.T) {
	eligible, realised := 0, 0
	for seed := int64(1); seed <= 400; seed++ {
		c, err := New(DefaultConfig(seed)).Generate()
		if err != nil {
			t.Fatal(err)
		}
		// The utf8-split arm is valid exactly when a string slice site
		// was reached with string_bytes in the mix.
		st := c.Stats["string-slice"]
		if st == nil || st.Valid["utf8-split"] == 0 {
			if hasFeature(c, "string_bytes") {
				t.Fatalf("seed %d: string_bytes realised without an eligible slice site", seed)
			}
			continue
		}
		eligible++
		if hasFeature(c, "string_bytes") {
			realised++
		}
	}
	t.Logf("string_bytes realised in %d of %d eligible cases (seeds 1-400)", realised, eligible)
	if eligible < 10 {
		t.Fatalf("only %d eligible cases in seeds 1-400: the string slice site or the mix changed", eligible)
	}
	if realised*3 < eligible {
		t.Fatalf("string_bytes realised in %d of %d eligible cases; want at least a third", realised, eligible)
	}
}
