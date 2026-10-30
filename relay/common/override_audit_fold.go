package common

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// paramOverrideArrayIndexRegexp matches a numeric array-index segment inside a
// dotted param-override path (the digits in `messages.15.role` or a trailing
// `foo.3`). It anchors on a leading dot and requires the digits to end at a
// path boundary (another dot, whitespace, or end of string) so numbers that are
// part of a value (for example `to user2`) are not treated as indices.
var paramOverrideArrayIndexRegexp = regexp.MustCompile(`\.(\d+)(?:\.|\s|$)`)

// foldParamOverrideAuditLines collapses audit lines that are identical except
// for numeric array indices in their path into compact range notation. For
// example the 56 lines
//
//	regex_replace messages.0.role from ^developer$ to user
//	... messages.1.role ... messages.55.role ...
//
// become a single
//
//	regex_replace messages.[0-55].role from ^developer$ to user
//
// When two (or more) positions carry numbers, folding is applied one dimension
// at a time, innermost first, and the result is always lossless — it never
// implies an index combination that did not actually occur. For the ragged set
//
//	delete messages.0.content.{0,1,2}  delete messages.{1,3,7}.content.{0,1}
//	delete messages.{2,4,5,6}.content.0
//
// the output is three lines (messages.0.content.[0-2], messages.[1,3,7].content.[0-1],
// messages.[2,4,5,6].content.0) rather than a false cartesian [0-7]x[0-2].
// Lines with no numeric path segment pass through unchanged; a "diagonal" set
// such as messages.0.content.0 + messages.1.content.1 is left as-is because it
// cannot be merged without inventing combinations. First-appearance order is
// preserved.
func foldParamOverrideAuditLines(lines []string) []string {
	if len(lines) < 2 {
		return lines
	}
	// Fold one index dimension per pass (innermost first); repeat until a pass
	// makes no further merges. The number of index dimensions on a single path
	// is tiny, so the loop converges in a handful of iterations; the cap is a
	// safety backstop only.
	current := lines
	for range 16 {
		next, changed := foldParamOverrideAuditLinesOnce(current)
		if !changed {
			return next
		}
		current = next
	}
	return current
}

// foldParamOverrideAuditLinesOnce folds a single index dimension: within each
// group of lines sharing the same all-indices-blanked template, it picks the
// last varying index position, sub-groups the lines by their values at every
// other position, and renders each sub-group's values at that position as a
// compact range (a single value stays a literal so later passes can still fold
// it). It returns the rewritten lines (first-appearance order preserved) and
// whether anything changed.
func foldParamOverrideAuditLinesOnce(lines []string) ([]string, bool) {
	const placeholder = "\x00"
	templates := make([]string, len(lines))
	indexValues := make([][]int, len(lines))
	groupOrder := make([]string, 0, len(lines))
	groups := make(map[string][]int, len(lines))
	for i, line := range lines {
		matches := paramOverrideArrayIndexRegexp.FindAllStringSubmatchIndex(line, -1)
		if len(matches) == 0 {
			continue
		}
		var sb strings.Builder
		prev := 0
		vals := make([]int, 0, len(matches))
		ok := true
		for _, m := range matches {
			digitStart, digitEnd := m[2], m[3]
			n, err := strconv.Atoi(line[digitStart:digitEnd])
			if err != nil {
				ok = false
				break
			}
			sb.WriteString(line[prev:digitStart])
			sb.WriteString(placeholder)
			prev = digitEnd
			vals = append(vals, n)
		}
		if !ok {
			continue // unparseable index (e.g. overflow): leave the line untouched
		}
		sb.WriteString(line[prev:])
		tpl := sb.String()
		templates[i] = tpl
		indexValues[i] = vals
		if _, seen := groups[tpl]; !seen {
			groupOrder = append(groupOrder, tpl)
		}
		groups[tpl] = append(groups[tpl], i)
	}

	emitted := make(map[int]string) // line index -> folded replacement (emit here)
	skip := make(map[int]bool)      // absorbed line indices
	changed := false
	for _, tpl := range groupOrder {
		members := groups[tpl]
		if len(members) < 2 {
			continue
		}
		numIdx := len(indexValues[members[0]])
		lastVarying := -1
		for pos := range numIdx {
			base := indexValues[members[0]][pos]
			for _, m := range members[1:] {
				if indexValues[m][pos] != base {
					lastVarying = pos
					break
				}
			}
		}
		if lastVarying == -1 {
			// Pure duplicates: keep the first, drop the rest.
			for _, m := range members[1:] {
				skip[m] = true
			}
			changed = true
			continue
		}
		parts := strings.Split(tpl, placeholder)
		// Sub-group members by their values at every position except lastVarying,
		// so each sub-group differs only in that one dimension (lossless fold).
		subOrder := make([]string, 0, len(members))
		subs := make(map[string][]int, len(members))
		for _, m := range members {
			var kb strings.Builder
			for pos, v := range indexValues[m] {
				if pos == lastVarying {
					kb.WriteByte('*')
					continue
				}
				kb.WriteString(strconv.Itoa(v))
				kb.WriteByte(',')
			}
			key := kb.String()
			if _, seen := subs[key]; !seen {
				subOrder = append(subOrder, key)
			}
			subs[key] = append(subs[key], m)
		}
		for _, key := range subOrder {
			sm := subs[key]
			first := sm[0]
			values := make([]int, 0, len(sm))
			for _, m := range sm {
				values = append(values, indexValues[m][lastVarying])
			}
			uniq := sortedUniqueInts(values)
			var sb strings.Builder
			for pos := range numIdx {
				sb.WriteString(parts[pos])
				if pos == lastVarying {
					if len(uniq) == 1 {
						sb.WriteString(strconv.Itoa(uniq[0]))
					} else {
						sb.WriteString("[" + compressIndexRanges(uniq) + "]")
					}
				} else {
					sb.WriteString(strconv.Itoa(indexValues[first][pos]))
				}
			}
			sb.WriteString(parts[numIdx])
			emitted[first] = sb.String()
			for _, m := range sm[1:] {
				skip[m] = true
			}
			if len(sm) > 1 {
				changed = true
			}
		}
	}

	result := make([]string, 0, len(lines))
	for i, line := range lines {
		if skip[i] {
			continue
		}
		if f, ok := emitted[i]; ok {
			result = append(result, f)
			continue
		}
		result = append(result, line)
	}
	return result, changed
}

// sortedUniqueInts returns the input values sorted ascending with duplicates
// removed. The input slice is not mutated.
func sortedUniqueInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	sorted := append([]int(nil), values...)
	slices.Sort(sorted)
	uniq := sorted[:1]
	for _, v := range sorted[1:] {
		if v != uniq[len(uniq)-1] {
			uniq = append(uniq, v)
		}
	}
	return uniq
}

// compressIndexRanges renders a set of array indices as a compact, sorted,
// de-duplicated string using dash ranges for runs of consecutive values,
// e.g. [1,2,3,4,5,6,7,8,9,11,15] -> "1-9,11,15".
func compressIndexRanges(values []int) string {
	uniq := sortedUniqueInts(values)
	if len(uniq) == 0 {
		return ""
	}
	var parts []string
	start := uniq[0]
	prev := uniq[0]
	flush := func() {
		if start == prev {
			parts = append(parts, strconv.Itoa(start))
		} else {
			parts = append(parts, strconv.Itoa(start)+"-"+strconv.Itoa(prev))
		}
	}
	for _, v := range uniq[1:] {
		if v == prev+1 {
			prev = v
			continue
		}
		flush()
		start = v
		prev = v
	}
	flush()
	return strings.Join(parts, ",")
}
