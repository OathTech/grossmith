package harness

// Report self-consistency (2026-08-10 audit, P0/P1). The evidence arc
// made `batch.json` INTEGRITY-checkable: complete.json digests it, so a
// reader can prove the bytes are the ones the producer wrote. It never
// made it MEANING-checkable — the CLI decoded the report with a plain
// Unmarshal and validated nothing, so a producer defect could write a
// self-contradictory statement, bind its bytes honestly, and receive a
// successful "verified". Cryptographic integrity is not semantic
// self-consistency, and `-verify` must state the two claims separately.
//
// What this file adds: a strict decoder, and a validator that recomputes
// every derivable field from the per-case records and the batch
// descriptor. It compares rather than trusts — totals, verdict
// histograms, wrapper accounting, subject digests, membership against
// the manifest, and each recorded verdict against what `Judge` returns
// for the recorded documents.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"grossmith/observe"
)

// ReadBatchReport strict-decodes a batch report. Unknown fields are a
// refusal: this is an evidence artifact, so a field the checker does not
// understand means the checker cannot vouch for the file.
func ReadBatchReport(root string) (BatchReport, error) {
	b, err := os.ReadFile(filepath.Join(root, "batch.json"))
	if err != nil {
		return BatchReport{}, err
	}
	var rep BatchReport
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rep); err != nil {
		return BatchReport{}, fmt.Errorf("batch.json: %w", err)
	}
	if dec.More() {
		return BatchReport{}, fmt.Errorf("batch.json: trailing content after the report")
	}
	if rep.Schema != BatchSchema {
		return BatchReport{}, fmt.Errorf("batch.json: schema %q, want %q", rep.Schema, BatchSchema)
	}
	return rep, nil
}

// readCaseFeatures reads the per-case feature tags from the case records
// the descriptor digests. A missing record prevents report verification:
// otherwise removing it would silently disable composition and wrapper checks.
func readCaseFeatures(root string, m Manifest) (map[string]map[string]int, error) {
	out := map[string]map[string]int{}
	for _, mc := range m.Cases {
		if _, listed := mc.Files["case.json"]; !listed {
			return nil, fmt.Errorf("report: case %s has no case.json record; composition and wrapper counts cannot be verified", mc.ID)
		}
		b, err := os.ReadFile(filepath.Join(root, mc.ID, "case.json"))
		if err != nil {
			return nil, err
		}
		var rec CaseRecord
		if err := json.Unmarshal(b, &rec); err != nil {
			return nil, fmt.Errorf("case %s record: %w", mc.ID, err)
		}
		if rec.ID != mc.ID {
			return nil, fmt.Errorf("case %s record names itself %q", mc.ID, rec.ID)
		}
		if rec.Schema != CaseSchema || rec.Seed != mc.Seed || rec.GeneratorRev != m.GeneratorRev {
			return nil, fmt.Errorf("case %s record schema, seed or generator revision disagrees with the manifest", mc.ID)
		}
		if rec.SubjectSHA256 != mc.Files["subject.go"] {
			return nil, fmt.Errorf("case %s record subject digest disagrees with the manifest", mc.ID)
		}
		if rec.DriverSHA256 != "" && rec.DriverSHA256 != mc.Files["driver.go"] {
			return nil, fmt.Errorf("case %s record driver digest disagrees with the manifest", mc.ID)
		}
		for tag, count := range rec.Features {
			if tag == "" || count <= 0 {
				return nil, fmt.Errorf("case %s record has invalid feature count %q=%d", mc.ID, tag, count)
			}
		}
		out[mc.ID] = rec.Features
	}
	return out, nil
}

// ValidateBatchReport checks a report against ITSELF, against the batch
// descriptor, and against the per-case records: every claim it makes
// that can be recomputed, is. The claims that CANNOT be recomputed from
// the batch — the adapter identity strings, the oracle version text, the
// budgets — are structural-checked only; they are bound by digest, so a
// reader knows they are the producer's bytes, not that they are true.
// Every case needs a manifested case.json record to verify composition
// and wrapper accounting. ValidateBatch can check source integrity alone
// for manually assembled batches without those records.
func ValidateBatchReport(root string, rep BatchReport, m Manifest) error {
	if rep.Schema != BatchSchema {
		return fmt.Errorf("report: schema %q, want %q", rep.Schema, BatchSchema)
	}
	if rep.GeneratorRev != m.GeneratorRev {
		return fmt.Errorf("report: generator revision %q differs from manifest %q", rep.GeneratorRev, m.GeneratorRev)
	}
	if rep.ReferenceName == "" {
		return fmt.Errorf("report: no reference name")
	}
	policy := observe.PanicPolicy(rep.PanicPolicy)
	ourPolicy := policy == observe.PanicExact || policy == observe.PanicKindOnly
	// The existing external adapter records an attestation, not clone
	// documents. Recognize its exact legacy policy; arbitrary policy text
	// must never disable re-judging of a direct comparison.
	externalPolicy := rep.CloneName == "golean" && rep.PanicPolicy == "golean-harness (expected status + exact panic message)"
	if !ourPolicy && !externalPolicy {
		return fmt.Errorf("report: unknown comparison policy %q for clone %q", rep.PanicPolicy, rep.CloneName)
	}
	if (rep.WrapperJudged == nil) != (rep.WrapperCloneInfra == nil) {
		return fmt.Errorf("report: wrapperJudged and wrapperCloneInfra must be present or absent together")
	}
	if rep.CloneName == "" && rep.WrapperJudged != nil {
		return fmt.Errorf("report: no clone judged, but wrapperJudged is present")
	}
	if rep.CloneName != "" && rep.WrapperJudged == nil {
		return fmt.Errorf("report: clone %s judged, but wrapperJudged is absent", rep.CloneName)
	}
	if rep.CloneName != "" && rep.CloneIdentity == "" {
		return fmt.Errorf("report: no clone identity")
	}
	// Membership: exactly the descriptor's cases, once each.
	declared := map[string]ManifestCase{}
	for _, mc := range m.Cases {
		declared[mc.ID] = mc
	}
	seen := map[string]bool{}
	for _, cr := range rep.Cases {
		if _, ok := declared[cr.ID]; !ok {
			return fmt.Errorf("report: case %s is not in the batch descriptor", cr.ID)
		}
		if seen[cr.ID] {
			return fmt.Errorf("report: duplicate case %s", cr.ID)
		}
		seen[cr.ID] = true
	}
	for _, mc := range m.Cases {
		if !seen[mc.ID] {
			return fmt.Errorf("report: declared case %s has no result", mc.ID)
		}
	}
	// Totals and the seed range are derivable, so they are checked, not
	// believed.
	if rep.Total != len(rep.Cases) {
		return fmt.Errorf("report: total %d but %d case results", rep.Total, len(rep.Cases))
	}
	if rep.Seeds[1] < rep.Seeds[0] {
		return fmt.Errorf("report: seed range [%d, %d] is inverted", rep.Seeds[0], rep.Seeds[1])
	}
	if len(m.Cases) > 0 {
		low, high := m.Cases[0].Seed, m.Cases[0].Seed
		for _, mc := range m.Cases[1:] {
			low, high = min(low, mc.Seed), max(high, mc.Seed)
		}
		if rep.Seeds != [2]int64{low, high} {
			return fmt.Errorf("report: seed range %v differs from manifest [%d, %d]", rep.Seeds, low, high)
		}
	}
	// Per-case structure, subject identity, and — where the policy is
	// ours — the recorded verdict itself.
	refRan, panicPaths, recovered := 0, 0, 0
	verdicts := map[Verdict]int{}
	for _, cr := range rep.Cases {
		if !digestRe.MatchString(cr.SubjectSHA256) {
			return fmt.Errorf("report: case %s subjectSha256 %q is not a sha256 digest", cr.ID, cr.SubjectSHA256)
		}
		// The subject the report names must be the subject the descriptor
		// digested — the join the audit found missing.
		if want := declared[cr.ID].Files["subject.go"]; want != cr.SubjectSHA256 {
			return fmt.Errorf("report: case %s names subject %s but the descriptor digests %s",
				cr.ID, ShortDigest(cr.SubjectSHA256), ShortDigest(want))
		}
		if err := validateOutcome(cr.ID, "reference", cr.Reference); err != nil {
			return err
		}
		if cr.Clone != nil {
			if err := validateOutcome(cr.ID, "clone", *cr.Clone); err != nil {
				return err
			}
		}
		if cr.Verdict != "" && !knownVerdict(cr.Verdict) {
			return fmt.Errorf("report: case %s has unknown verdict %q", cr.ID, cr.Verdict)
		}
		switch {
		case rep.CloneName == "" && (cr.Clone != nil || cr.Verdict != ""):
			return fmt.Errorf("report: case %s carries clone evidence or a verdict without a clone", cr.ID)
		case rep.CloneName != "" && cr.Verdict == "":
			return fmt.Errorf("report: case %s has no clone verdict", cr.ID)
		case rep.CloneName != "" && ourPolicy && cr.Clone == nil:
			return fmt.Errorf("report: case %s has no clone outcome for a direct comparison", cr.ID)
		case externalPolicy && cr.Clone != nil:
			return fmt.Errorf("report: case %s carries a direct clone outcome under an external policy", cr.ID)
		}
		if cr.Verdict != "" {
			verdicts[cr.Verdict]++
		}
		if cr.Reference.Status == StatusRan {
			refRan++
			switch cr.Reference.Document.Status {
			case observe.StatusPanic:
				panicPaths++
			}
			for _, e := range cr.Reference.Document.Events {
				if e.At == "recovered" {
					recovered++
				}
			}
		}
		// RE-JUDGE: the verdict a reader would compute from the recorded
		// documents must be the verdict the report claims. Only possible
		// when the comparison was OURS — a clone whose own harness applied
		// its equivalence (golean pins exact panic messages externally)
		// records a conclusion we cannot recompute, which is its own
		// audit finding and is stated, not papered over.
		if ourPolicy && cr.Clone != nil && cr.Verdict != "" {
			want, _ := Judge(cr.Reference, *cr.Clone, policy)
			if want != cr.Verdict {
				return fmt.Errorf("report: case %s records verdict %s but its own documents judge %s",
					cr.ID, cr.Verdict, want)
			}
		}
	}
	// Aggregates.
	if rep.RefRan != refRan {
		return fmt.Errorf("report: refRan %d but %d cases ran on the reference", rep.RefRan, refRan)
	}
	if rep.PanicPaths != panicPaths {
		return fmt.Errorf("report: panicPaths %d but %d reference documents report a panic", rep.PanicPaths, panicPaths)
	}
	if rep.Recovered != recovered {
		return fmt.Errorf("report: recovered %d but %d recovered events are recorded", rep.Recovered, recovered)
	}
	if err := sameHistogram(rep.Verdicts, verdicts); err != nil {
		return fmt.Errorf("report: verdict histogram disagrees with the case results: %w", err)
	}
	// The case-record join: with per-case feature tags, the composition
	// histogram and the wrapper counts become derivable too — the
	// wrapper trio was otherwise unchecked on clone-less batches (found
	// while probing this validator with rewritten reports).
	features, err := readCaseFeatures(root, m)
	if err != nil {
		return err
	}
	composition := map[string]int{}
	judgedComposition := map[string]int{}
	wrapperCaught, wrapperJudged, wrapperInfra := 0, 0, 0
	for _, cr := range rep.Cases {
		judged := cr.Verdict == VerdictMatch || cr.Verdict == VerdictMismatch
		for tag := range features[cr.ID] {
			composition[tag]++
			if judged {
				judgedComposition[tag]++
			}
		}
		if features[cr.ID]["recover_wrapper"] == 0 || cr.Reference.Status != StatusRan {
			continue
		}
		// The producer's rule, recomputed: a wrapped subject whose
		// reference run returned ok with a nonzero trailing panic-site
		// slot caught a panic.
		doc := cr.Reference.Document
		if doc.Status == observe.StatusOK && len(doc.Values) > 0 {
			if last := doc.Values[len(doc.Values)-1]; last.Kind == "int" && last.Int != 0 {
				wrapperCaught++
				if judged {
					wrapperJudged++
				}
				if cr.Verdict == VerdictCloneInfra {
					wrapperInfra++
				}
			}
		}
	}
	if rep.WrapperCaught != wrapperCaught {
		return fmt.Errorf("report: wrapperCaught %d but %d wrapped subjects caught a panic in their recorded documents",
			rep.WrapperCaught, wrapperCaught)
	}
	if rep.WrapperJudged != nil && (*rep.WrapperJudged != wrapperJudged || *rep.WrapperCloneInfra != wrapperInfra) {
		return fmt.Errorf("report: wrapper legs disagree with case verdicts: judged %d (want %d), cloneInfra %d (want %d)",
			*rep.WrapperJudged, wrapperJudged, *rep.WrapperCloneInfra, wrapperInfra)
	}
	if err := sameFeatureHistogram(rep.CompositionJudged, judgedComposition); err != nil {
		return fmt.Errorf("report: compositionJudged %w", err)
	}
	// Composition is DERIVABLE once the case records are readable, so
	// it is required to match exactly — including presence. An empty
	// or absent histogram beside cases that carry tags is a false
	// report, not an unverifiable one (found while probing: the
	// earlier `len(...) > 0` guard let a report that dropped every tag
	// pass, because dropping them all made the map empty).
	for tag, claimed := range rep.Composition {
		if composition[tag] != claimed {
			return fmt.Errorf("report: composition[%s] claimed %d, computed %d from the case records",
				tag, claimed, composition[tag])
		}
	}
	for tag, computed := range composition {
		if _, ok := rep.Composition[tag]; !ok && computed > 0 {
			return fmt.Errorf("report: composition omits %s, which %d cases carry", tag, computed)
		}
	}
	// Wrapper accounting: the three-leg identity the nightly gates on
	// (caught == judged + cloneInfra, exactly, when a clone judged).
	if rep.WrapperJudged != nil && rep.WrapperCloneInfra != nil {
		if *rep.WrapperJudged < 0 || *rep.WrapperCloneInfra < 0 ||
			*rep.WrapperJudged > rep.Total || *rep.WrapperCloneInfra > rep.Total {
			return fmt.Errorf("report: wrapper counts are outside [0, total]")
		}
		if got := *rep.WrapperJudged + *rep.WrapperCloneInfra; got != rep.WrapperCaught {
			return fmt.Errorf("report: wrapperCaught %d but judged %d + cloneInfra %d = %d",
				rep.WrapperCaught, *rep.WrapperJudged, *rep.WrapperCloneInfra, got)
		}
	}
	// Identities and oracles: present, and digest-shaped where they are
	// digests.
	if rep.ReferenceIdentity == "" {
		return fmt.Errorf("report: no reference identity")
	}
	for name, o := range map[string]*OracleIdentity{
		"referenceOracle": rep.ReferenceOracle, "cloneNestedOracle": rep.CloneNestedOracle,
		"cloneOracle": rep.CloneOracle,
	} {
		if o == nil {
			continue
		}
		if !digestRe.MatchString(o.SHA256) {
			return fmt.Errorf("report: %s sha256 %q is not a sha256 digest", name, o.SHA256)
		}
		if o.ScriptSHA256 != "" && !digestRe.MatchString(o.ScriptSHA256) {
			return fmt.Errorf("report: %s scriptSha256 %q is not a sha256 digest", name, o.ScriptSHA256)
		}
		if o.Path == "" || o.Version == "" {
			return fmt.Errorf("report: %s names no path or version", name)
		}
	}
	for id, d := range rep.CloneWorkFiles {
		if !digestRe.MatchString(d) {
			return fmt.Errorf("report: cloneWorkFiles[%s] %q is not a sha256 digest", id, d)
		}
	}
	if rep.CloneName == "" && (rep.CloneIdentity != "" || rep.CloneOracle != nil || rep.CloneNestedOracle != nil) {
		return fmt.Errorf("report: clone identity recorded without a clone")
	}
	return nil
}

// validateOutcome checks one recorded outcome's structure: a ran outcome
// carries a valid document, a non-ran one carries a reason.
func validateOutcome(id, side string, o Outcome) error {
	if err := o.Validate(); err != nil {
		return fmt.Errorf("report: case %s %s %w", id, side, err)
	}
	return nil
}

func knownVerdict(v Verdict) bool {
	switch v {
	case VerdictMatch, VerdictMismatch, VerdictRefInfra, VerdictCloneInfra,
		VerdictBothInfra, VerdictHarnessError:
		return true
	}
	return false
}

func sameHistogram(claimed, computed map[Verdict]int) error {
	keys := map[Verdict]bool{}
	for k := range claimed {
		keys[k] = true
	}
	for k := range computed {
		keys[k] = true
	}
	var names []string
	for k := range keys {
		names = append(names, string(k))
	}
	sort.Strings(names)
	for _, n := range names {
		v := Verdict(n)
		if !knownVerdict(v) {
			return fmt.Errorf("unknown verdict %q", v)
		}
		if claimed[v] != computed[v] {
			return fmt.Errorf("%s claimed %d, computed %d", n, claimed[v], computed[v])
		}
	}
	return nil
}

func sameFeatureHistogram(claimed, computed map[string]int) error {
	keys := map[string]bool{}
	for k := range claimed {
		keys[k] = true
	}
	for k := range computed {
		keys[k] = true
	}
	var names []string
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if claimed[k] != computed[k] {
			return fmt.Errorf("[%s] claimed %d, computed %d", k, claimed[k], computed[k])
		}
	}
	return nil
}
