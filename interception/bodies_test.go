package interception

import (
	"io"
	"io/ioutil"
	"reflect"
	"strings"
	"testing"
)

func TestBodyReadCloser(t *testing.T) {
	tests := []struct {
		name       string
		data       string
		bufferSize int
		peekSize   int
	}{
		{`empty with peek`, ``, 10, 10},
		{`empty NO peek`, ``, 10, 0},
		{`read smaller than peek`, `0123456789`, 5, 10},
		{`read equal to peek`, `0123456789`, 10, 10},
		{`read larger than peek`, `0123456789`, 10, 5},
		{`data smaller than buffers`, `01234`, 6, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			brc := NewBodyReadCloser(ioutil.NopCloser(strings.NewReader(tt.data)), tt.peekSize)
			length := len(tt.data)

			bufferSize := tt.bufferSize
			if bufferSize == -1 {
				bufferSize = length
			}

			peekSize := tt.peekSize
			if length < peekSize {
				peekSize = length
			}

			final := make([]byte, length)
			buffer := make([]byte, bufferSize)

			totalRead := 0
			i := 0

			for {
				n, err := brc.Read(buffer)
				if err != nil && err != io.EOF {
					t.Errorf(`Read() returned error %s`, err)
				}
				totalRead += n

				if n > 0 {
					copy(final[i*bufferSize:(i*bufferSize)+n], buffer[:n])
				}
				if err == io.EOF {
					break
				}

				i++
			}

			if totalRead != length {
				t.Errorf(`Read() expected total read: %d, actual: %d`, length, totalRead)
			}

			actual := string(final)
			if actual != tt.data {
				t.Errorf(`Read() expected: %v, actual: %v`, tt.data, actual)
			}

			buffer, _ = brc.Peek()
			actual = string(buffer)
			if actual != tt.data[:peekSize] {
				t.Errorf(`Peek() expected: %v, actual: %v`, tt.data[:10], actual)
			}
		})
	}
}

func TestParseFormData(t *testing.T) {
	tests := []struct {
		name     string
		data     io.Reader
		expected map[string][]string
		wantErr  bool
	}{
		{`happy`, strings.NewReader(`x=1&y=2&y=3`), map[string][]string{
			`x`: []string{`1`},
			`y`: []string{`2`, `3`},
		}, false},
		{`sad`, strings.NewReader(`%INVALID`), nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := ParseFormData(tt.data)

			if (err != nil) != tt.wantErr {
				t.Errorf("expected error %v but error %v", tt.wantErr, err != nil)
			}

			if err == nil && !reflect.DeepEqual(actual, tt.expected) {
				t.Errorf("expected: %#v, actual: %#v", tt.expected, actual)
			}
		})
	}
}
