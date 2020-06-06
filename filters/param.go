package filters

import (
	"errors"
	"fmt"
	"net/http"
)

// ParamFilter provides a key-value filter for API request parameters.
type ParamFilter struct {
	KeyValueMatcher
}

// Type is part of the Filter interface.
func (*ParamFilter) Type() FilterType {
	return ParamFilterType
}

// MatchesCall is part of the Filter interface.
func (f *ParamFilter) MatchesCall(r *http.Request, _ *http.Response) bool {
	m := NewKeyValueMatcher(f.KeyRegexp().String(), f.ValueRegexp().String())
	if r.URL == nil {
		return false
	}
	return m.Matches(r.URL.Query())
}

// SetMatcher sets the filter KeyValueMatcher.
//
// If the returned error is not nil, the filter Regex will accept any value.
//
// To apply a case-insensitive match, prepend (?i) to the matcher regexps,
// as in: (?i)\.bearer\.sh$
func (f *ParamFilter) SetMatcher(matcher Matcher) error {
	defaultMatcher := NewKeyValueMatcher(``, ``)

	m, ok := matcher.(KeyValueMatcher)
	if !ok {
		f.KeyValueMatcher = defaultMatcher
		return fmt.Errorf("key-value matcher expected, got a %T", matcher)
	}

	if isNilInterface(m) {
		f.KeyValueMatcher = defaultMatcher
		return errors.New("set nil Key-Value matcher on Param filter")
	}

	f.KeyValueMatcher = m
	return nil
}

func paramFilterFromDescription(filterMap FilterMap, d interface{}) Filter {
	kvd, ok := d.(KeyValueDescription)
	if !ok {
		return nil
	}
	// FIXME apply RegexpMatcherDescription.Flags.
	m := NewKeyValueMatcher(kvd.KeyPattern.Value, kvd.ValuePattern.Value)
	if m == nil {
		return nil
	}
	f := &ParamFilter{}
	err := f.SetMatcher(m)
	if err != nil {
		return nil
	}
	return f
}
