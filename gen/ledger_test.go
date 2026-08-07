package gen

import (
	"os"
	"strings"
	"testing"
)

// TestLedgerNamesEveryTag is the ledger-to-code honesty gate (Phase 4
// rung 0): every optional construct tag must appear (backticked) in
// docs/spec-ledger.md, so a new construct cannot land without its ledger
// row and a removed one cannot linger as phantom coverage.
func TestLedgerNamesEveryTag(t *testing.T) {
	b, err := os.ReadFile("../docs/spec-ledger.md")
	if err != nil {
		t.Fatal(err)
	}
	ledger := string(b)
	for _, tag := range Optional() {
		if !strings.Contains(ledger, "`"+tag+"`") {
			t.Errorf("optional construct %q has no spec-ledger row", tag)
		}
	}
}

// TestSubjectSizeBudget (audit M4, explicitly gated on ladder
// resumption): the one-per-type observed floor grows with every type
// rung, and "small programs" is a charter claim — so the size
// distribution is a witnessed budget, not a hope. A rung that grows the
// floor must bump these numbers CONSCIOUSLY, in the same commit, with
// the ledger row explaining why.
func TestSubjectSizeBudget(t *testing.T) {
	const n = 100
	sum, max := 0, 0
	for seed := int64(88000); seed < 88000+n; seed++ {
		c := generate(t, seed)
		sz := len(c.Source)
		sum += sz
		if sz > max {
			max = sz
		}
	}
	mean := sum / n
	t.Logf("subject bytes over %d seeds: mean %d, max %d", n, mean, max)
	if mean > 2800 {
		t.Errorf("mean subject size %d exceeds the 2800-byte budget", mean)
	}
	if max > 7000 {
		t.Errorf("max subject size %d exceeds the 7000-byte budget", max)
	}
}
