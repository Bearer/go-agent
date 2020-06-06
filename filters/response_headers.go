package filters

import (
	"errors"
	"fmt"
	"net/http"
)

// ResponseHeadersFilter provides a filter for API Response headers.
type ResponseHeadersFilter struct {
	KeyValueMatcher
}

// Type is part of the Filter interface.
func (f *ResponseHeadersFilter) Type() FilterType {
	return ResponseHeadersFilterType
}

func (f *ResponseHeadersFilter) ensureMatcher() {
	if !isNilInterface(f.KeyValueMatcher) {
		return
	}
	_ = f.SetMatcher(NewKeyValueMatcher(``, ``))
}

// MatchesCall is part of the Filter interface.
func (f *ResponseHeadersFilter) MatchesCall(_ *http.Request, response *http.Response) bool {
	f.ensureMatcher()
	return f.KeyValueMatcher.Matches(response.Header)
}

// SetMatcher sets the filter KeyValueMatcher.
//
// If the returned error is not nil, the filter will accept any value except nil.
//
// To apply a case-insensitive match, prepend (?i) to the matcher regexps,
// as in: (?i)\.bearer\.sh$
func (f *ResponseHeadersFilter) SetMatcher(matcher Matcher) error {
	defaultMatcher := NewKeyValueMatcher(``, ``)

	m, ok := matcher.(KeyValueMatcher)
	if !ok {
		f.KeyValueMatcher = defaultMatcher
		return fmt.Errorf("key-value matcher expected, got a %T", matcher)
	}

	if isNilInterface(m) {
		f.KeyValueMatcher = defaultMatcher
		return errors.New("set nil Key-Value matcher on ResponseHeaders filter")
	}

	f.KeyValueMatcher = m
	return nil
}

func responseHeadersFilterFromDescription(filterMap FilterMap, fd *FilterDescription) Filter {
	// FIXME apply RegexpMatcherDescription.Flags
	m := NewKeyValueMatcher(fd.KeyPattern.Value, fd.ValuePattern.Value)
	if m == nil {
		return nil
	}
	f := &ResponseHeadersFilter{}
	err := f.SetMatcher(m)
	if err != nil {
		return nil
	}
	return f
}
