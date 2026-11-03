package common

import (
	"bytes"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/tidwall/gjson"
)

type compiledKeyRenameRule struct {
	path []string
	keys map[string]string
}

// keyRenamer rewrites object keys of a validated JSON document in a single
// copy pass. Everything except a renamed key (values, key order, whitespace)
// is copied byte-for-byte, so numbers and strings can never be altered. The
// output buffer stays unallocated until the first rename, so a body that
// matches no rule is never copied.
type keyRenamer struct {
	data  []byte
	rules []compiledKeyRenameRule
	out   bytes.Buffer
	// copied is the end offset of the input accounted for so far. The rewrite
	// walks the document in order, so until a rename happens the output owed to
	// the caller is exactly data[:copied] and does not need to be materialized.
	copied int
	// started reports whether out holds the rewritten document.
	started bool
	// audit lists every applied rename as "rename <original path> -> <new key>".
	audit []string
}

type jsonObjectMember struct {
	keyStart, keyEnd, valueStart int
	key                          string
}

// RenameRequestKeys applies a channel's RequestKeyRename rules to a JSON
// request body and returns the rewritten body with one audit line per applied
// rename ("rename contents.0.parts.1.file_data -> fileData"), array indices
// folded into ranges like param-override audit lines. It returns the body
// unchanged and no audit lines when no rule applies or the body is not valid
// JSON. When several rules rename the same
// key at the same path, the later rule wins. A rename is skipped when the
// target key already exists in the same object, so an explicit client value
// wins.
func RenameRequestKeys(data []byte, rules []dto.RequestKeyRenameRule) ([]byte, []string, error) {
	compiled := make([]compiledKeyRenameRule, 0, len(rules))
	for _, rule := range rules {
		keys := make(map[string]string, len(rule.Keys))
		for from, to := range rule.Keys {
			if from == "" || to == "" || from == to {
				continue
			}
			keys[from] = to
		}
		if len(keys) == 0 {
			continue
		}
		path := strings.TrimPrefix(strings.TrimSpace(rule.Path), "$")
		path = strings.TrimPrefix(path, ".")
		var segments []string
		if path != "" {
			segments = strings.Split(path, ".")
		}
		compiled = append(compiled, compiledKeyRenameRule{path: segments, keys: keys})
	}
	if len(compiled) == 0 || !keyRenameMayApply(data, compiled) || !gjson.ValidBytes(data) {
		return data, nil, nil
	}

	r := &keyRenamer{data: data, rules: compiled}
	start := r.skipSpace(0)
	r.copyInput(0, start)
	end, err := r.rewriteValue(start, nil)
	if err != nil {
		return data, nil, err
	}
	if !r.started {
		return data, nil, nil
	}
	r.copyInput(end, len(data))
	return r.out.Bytes(), foldParamOverrideAuditLines(r.audit), nil
}

// keyRenameMayApply reports whether data can hold an object key that one of the
// rules renames. A key written literally appears in the document as the token
// `"key"`, so scanning the quoted tokens proves the absence of every rename
// without parsing or copying the body. JSON also allows a key to carry \u
// escapes, which the scan cannot resolve. A document containing any \u escape,
// or a rule key that cannot appear literally, therefore reports true and falls
// through to the full rewrite.
func keyRenameMayApply(data []byte, rules []compiledKeyRenameRule) bool {
	for _, rule := range rules {
		for from := range rule.keys {
			if strings.ContainsFunc(from, func(ch rune) bool {
				return ch < 0x20 || ch == '"' || ch == '\\' || ch == '/'
			}) {
				return true
			}
		}
	}
	if bytes.Contains(data, []byte(`\u`)) {
		return true
	}
	// Every quote is a candidate key start, and the keys kept above contain no
	// quote, so the next quote closes the candidate. Each gap between two quotes
	// is scanned a bounded number of times, keeping this a single pass over the
	// body no matter how many keys the rules rename.
	for i := 0; i < len(data); {
		offset := bytes.IndexByte(data[i:], '"')
		if offset < 0 {
			return false
		}
		keyStart := i + offset + 1
		offset = bytes.IndexByte(data[keyStart:], '"')
		if offset < 0 {
			return false
		}
		token := data[keyStart : keyStart+offset]
		for _, rule := range rules {
			if _, ok := rule.keys[string(token)]; ok {
				return true
			}
		}
		i = keyStart
	}
	return false
}

// copyInput accounts for data[from:to], which the rewrite reproduces verbatim.
// The bytes are written only once out holds a document that differs from the
// input; before that, recording the offset is enough.
func (r *keyRenamer) copyInput(from, to int) {
	if r.started {
		r.out.Write(r.data[from:to])
	}
	r.copied = to
}

// startOutput allocates the output buffer at the first rename and seeds it with
// the unchanged prefix accounted for so far.
func (r *keyRenamer) startOutput() {
	r.out.Grow(len(r.data))
	r.out.Write(r.data[:r.copied])
	r.started = true
}

// keyPathMatches reports whether path matches pattern exactly.
func keyPathMatches(pattern, path []string) bool {
	if len(pattern) == 0 {
		return len(path) == 0
	}
	if pattern[0] == "**" {
		for skip := range len(path) + 1 {
			if keyPathMatches(pattern[1:], path[skip:]) {
				return true
			}
		}
		return false
	}
	if len(path) == 0 || (pattern[0] != "*" && pattern[0] != path[0]) {
		return false
	}
	return keyPathMatches(pattern[1:], path[1:])
}

// keyPathMayMatchWithin reports whether path itself or some descendant of it
// can match pattern, i.e. whether the subtree at path needs to be walked.
func keyPathMayMatchWithin(pattern, path []string) bool {
	if len(path) == 0 {
		return true
	}
	if len(pattern) == 0 {
		return false
	}
	if pattern[0] == "**" {
		return true
	}
	if pattern[0] != "*" && pattern[0] != path[0] {
		return false
	}
	return keyPathMayMatchWithin(pattern[1:], path[1:])
}

func (r *keyRenamer) skipSpace(i int) int {
	for i < len(r.data) {
		switch r.data[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

func (r *keyRenamer) skipString(i int) (int, error) {
	for j := i + 1; j < len(r.data); j++ {
		switch r.data[j] {
		case '\\':
			j++
		case '"':
			return j + 1, nil
		}
	}
	return 0, fmt.Errorf("unterminated string at offset %d", i)
}

func (r *keyRenamer) skipValue(i int) (int, error) {
	if i >= len(r.data) {
		return 0, fmt.Errorf("unexpected end of JSON")
	}
	switch r.data[i] {
	case '"':
		return r.skipString(i)
	case '{', '[':
		depth := 0
		for j := i; j < len(r.data); j++ {
			switch r.data[j] {
			case '"':
				end, err := r.skipString(j)
				if err != nil {
					return 0, err
				}
				j = end - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return j + 1, nil
				}
			}
		}
		return 0, fmt.Errorf("unterminated container at offset %d", i)
	default:
		j := i
		for j < len(r.data) && !strings.ContainsRune(",}] \t\n\r", rune(r.data[j])) {
			j++
		}
		return j, nil
	}
}

// rewriteValue writes the value starting at i to r.out and returns its end.
// Containers are only walked when some rule can match inside them; otherwise
// they are copied verbatim.
func (r *keyRenamer) rewriteValue(i int, path []string) (int, error) {
	if i < len(r.data) && (r.data[i] == '{' || r.data[i] == '[') {
		for _, rule := range r.rules {
			if !keyPathMayMatchWithin(rule.path, path) {
				continue
			}
			if r.data[i] == '{' {
				return r.rewriteObject(i, path)
			}
			return r.rewriteArray(i, path)
		}
	}
	end, err := r.skipValue(i)
	if err != nil {
		return 0, err
	}
	r.copyInput(i, end)
	return end, nil
}

func (r *keyRenamer) rewriteObject(start int, path []string) (int, error) {
	var members []jsonObjectMember
	existing := make(map[string]bool)
	i := r.skipSpace(start + 1)
	for i < len(r.data) && r.data[i] != '}' {
		keyEnd, err := r.skipString(i)
		if err != nil {
			return 0, err
		}
		member := jsonObjectMember{keyStart: i, keyEnd: keyEnd, key: string(r.data[i+1 : keyEnd-1])}
		if bytes.IndexByte(r.data[i:keyEnd], '\\') >= 0 {
			if err := common.Unmarshal(r.data[i:keyEnd], &member.key); err != nil {
				return 0, err
			}
		}
		i = r.skipSpace(keyEnd)
		if i >= len(r.data) || r.data[i] != ':' {
			return 0, fmt.Errorf("expected ':' at offset %d", i)
		}
		member.valueStart = r.skipSpace(i + 1)
		valueEnd, err := r.skipValue(member.valueStart)
		if err != nil {
			return 0, err
		}
		members = append(members, member)
		existing[member.key] = true
		i = r.skipSpace(valueEnd)
		if i < len(r.data) && r.data[i] == ',' {
			i = r.skipSpace(i + 1)
		}
	}
	if i >= len(r.data) {
		return 0, fmt.Errorf("unterminated object at offset %d", start)
	}

	renames := make(map[string]string)
	for _, rule := range r.rules {
		if !keyPathMatches(rule.path, path) {
			continue
		}
		maps.Copy(renames, rule.keys)
	}

	prev := start
	for _, member := range members {
		r.copyInput(prev, member.keyStart)
		to, rename := renames[member.key]
		if rename && !existing[to] {
			encoded, err := common.Marshal(to)
			if err != nil {
				return 0, err
			}
			if !r.started {
				r.startOutput()
			}
			r.out.Write(encoded)
			r.copied = member.keyEnd
			existing[to] = true
			r.audit = append(r.audit, "rename "+strings.Join(append(path, member.key), ".")+" -> "+to)
		} else {
			r.copyInput(member.keyStart, member.keyEnd)
		}
		r.copyInput(member.keyEnd, member.valueStart)
		end, err := r.rewriteValue(member.valueStart, append(path, member.key))
		if err != nil {
			return 0, err
		}
		prev = end
	}
	r.copyInput(prev, i+1)
	return i + 1, nil
}

func (r *keyRenamer) rewriteArray(start int, path []string) (int, error) {
	prev := start + 1
	r.copyInput(start, prev)
	i := r.skipSpace(start + 1)
	for index := 0; i < len(r.data) && r.data[i] != ']'; index++ {
		r.copyInput(prev, i)
		end, err := r.rewriteValue(i, append(path, strconv.Itoa(index)))
		if err != nil {
			return 0, err
		}
		prev = end
		i = r.skipSpace(end)
		if i < len(r.data) && r.data[i] == ',' {
			i = r.skipSpace(i + 1)
		}
	}
	if i >= len(r.data) {
		return 0, fmt.Errorf("unterminated array at offset %d", start)
	}
	r.copyInput(prev, i+1)
	return i + 1, nil
}
