// Copyright 2017 The Bazel Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package starlark_test

// This file defines tests of the Value API.

import (
	"fmt"
	"iter"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.starlark.net/starlark"
)

func TestStringMethod(t *testing.T) {
	s := starlark.String("hello")
	for i, test := range [][2]string{
		// quoted string:
		{s.String(), `"hello"`},
		{fmt.Sprintf("%s", s), `"hello"`},
		{fmt.Sprintf("%+s", s), `"hello"`},
		{fmt.Sprintf("%v", s), `"hello"`},
		{fmt.Sprintf("%+v", s), `"hello"`},
		// unquoted:
		{s.GoString(), `hello`},
		{fmt.Sprintf("%#v", s), `hello`},
	} {
		got, want := test[0], test[1]
		if got != want {
			t.Errorf("#%d: got <<%s>>, want <<%s>>", i, got, want)
		}
	}
}

func TestListAppend(t *testing.T) {
	l := starlark.NewList(nil)
	l.Append(starlark.String("hello"))
	res, ok := starlark.AsString(l.Index(0))
	if !ok {
		t.Errorf("failed list.Append() got: %s, want: starlark.String", l.Index(0).Type())
	}
	if res != "hello" {
		t.Errorf("failed list.Append() got: %+v, want: hello", res)
	}
}

func TestParamDefault(t *testing.T) {
	tests := []struct {
		desc         string
		starFn       string
		wantDefaults []starlark.Value
	}{
		{
			desc:         "function with all required params",
			starFn:       "all_required",
			wantDefaults: []starlark.Value{nil, nil, nil},
		},
		{
			desc:   "function with all optional params",
			starFn: "all_opt",
			wantDefaults: []starlark.Value{
				starlark.String("a"),
				starlark.None,
				starlark.String(""),
			},
		},
		{
			desc:   "function with required and optional params",
			starFn: "mix_required_opt",
			wantDefaults: []starlark.Value{
				nil,
				nil,
				starlark.String("c"),
				starlark.String("d"),
			},
		},
		{
			desc:   "function with required, optional, and varargs params",
			starFn: "with_varargs",
			wantDefaults: []starlark.Value{
				nil,
				starlark.String("b"),
				nil,
			},
		},
		{
			desc:   "function with required, optional, varargs, and keyword-only params",
			starFn: "with_varargs_kwonly",
			wantDefaults: []starlark.Value{
				nil,
				starlark.String("b"),
				starlark.String("c"),
				nil,
				nil,
			},
		},
		{
			desc:   "function with required, optional, and keyword-only params",
			starFn: "with_kwonly",
			wantDefaults: []starlark.Value{
				nil,
				starlark.String("b"),
				starlark.String("c"),
				nil,
			},
		},
		{
			desc:   "function with required, optional, and kwargs params",
			starFn: "with_kwargs",
			wantDefaults: []starlark.Value{
				nil,
				starlark.String("b"),
				starlark.String("c"),
				nil,
			},
		},
		{
			desc:   "function with required, optional, varargs, kw-only, and kwargs params",
			starFn: "with_varargs_kwonly_kwargs",
			wantDefaults: []starlark.Value{
				nil,
				starlark.String("b"),
				starlark.String("c"),
				nil,
				starlark.String("e"),
				nil,
				nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			thread := &starlark.Thread{}
			filename := "testdata/function_param.star"
			globals, err := starlark.ExecFile(thread, filename, nil, nil)
			if err != nil {
				t.Fatalf("ExecFile(%v, %q) failed: %v", thread, filename, err)
			}

			fn, ok := globals[tt.starFn].(*starlark.Function)
			if !ok {
				t.Fatalf("value %v is not a Starlark function", globals[tt.starFn])
			}

			var paramDefaults []starlark.Value
			for i := 0; i < fn.NumParams(); i++ {
				paramDefaults = append(paramDefaults, fn.ParamDefault(i))
			}
			if diff := cmp.Diff(tt.wantDefaults, paramDefaults); diff != "" {
				t.Errorf("param defaults got diff (-want +got):\n%s", diff)
			}
		})
	}
}

// A fakeMapping is a starlark.IterableMapping that deliberately does not
// implement the Elements or Entries fast paths, so that the standalone
// starlark.Elements and starlark.Entries functions must use the generic
// Iterate/Done code path.
//
// Like *List and *Dict it counts its active iterators, but as a signed
// integer so that the tests can observe underflow.
type fakeMapping struct {
	items     []starlark.Tuple
	itercount int
}

var _ starlark.IterableMapping = (*fakeMapping)(nil)

func (m *fakeMapping) String() string          { return "fakemapping" }
func (m *fakeMapping) Type() string            { return "fakemapping" }
func (m *fakeMapping) Freeze()                 {}
func (m *fakeMapping) Truth() starlark.Bool    { return starlark.Bool(len(m.items) > 0) }
func (m *fakeMapping) Hash() (uint32, error)   { return 0, fmt.Errorf("unhashable: %s", m.Type()) }
func (m *fakeMapping) Items() []starlark.Tuple { return m.items }

func (m *fakeMapping) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	for _, item := range m.items {
		if eq, err := starlark.Equal(item[0], k); err != nil {
			return nil, false, err
		} else if eq {
			return item[1], true, nil
		}
	}
	return nil, false, nil
}

func (m *fakeMapping) Iterate() starlark.Iterator {
	m.itercount++
	return &fakeMappingIterator{m: m}
}

type fakeMappingIterator struct {
	m *fakeMapping
	i int
}

func (it *fakeMappingIterator) Next(p *starlark.Value) bool {
	if it.i == len(it.m.items) {
		return false
	}
	*p = it.m.items[it.i][0]
	it.i++
	return true
}

func (it *fakeMappingIterator) Done() { it.m.itercount-- }

func assertNoElementsFastPath(t *testing.T, v any) {
	t.Helper()
	if _, ok := v.(interface {
		Elements() iter.Seq[starlark.Value]
	}); ok {
		t.Fatalf("%T has an Elements fast path, so this test would not exercise Elements' generic code path", v)
	}
}

func assertNoEntriesFastPath(t *testing.T, v any) {
	t.Helper()
	if _, ok := v.(interface {
		Entries() iter.Seq2[starlark.Value, starlark.Value]
	}); ok {
		t.Fatalf("%T has an Entries fast path, so this test would not exercise Entries' generic code path", v)
	}
}

func TestElementsIteratorCount(t *testing.T) {
	m := &fakeMapping{items: []starlark.Tuple{
		{starlark.String("one"), starlark.MakeInt(1)},
		{starlark.String("two"), starlark.MakeInt(2)},
	}}
	assertNoElementsFastPath(t, m)

	// Asking for the sequence must not start an iteration.
	seq := starlark.Elements(m)
	if m.itercount != 0 {
		t.Errorf("Elements(m) started an iteration: itercount = %d, want 0", m.itercount)
	}

	// Each iteration must be independent and must balance Iterate with Done.
	want := []string{`"one"`, `"two"`}
	for pass := range 2 {
		var got []string
		for elem := range seq {
			got = append(got, fmt.Sprint(elem))
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("pass %d: Elements(m) diff (-want +got):\n%s", pass, diff)
		}
		if m.itercount != 0 {
			t.Errorf("pass %d: unbalanced Iterate/Done: itercount = %d, want 0", pass, m.itercount)
		}
	}
}

func TestEntriesIteratorCount(t *testing.T) {
	m := &fakeMapping{items: []starlark.Tuple{
		{starlark.String("one"), starlark.MakeInt(1)},
		{starlark.String("two"), starlark.MakeInt(2)},
	}}
	assertNoEntriesFastPath(t, m)

	// Asking for the sequence must not start an iteration.
	seq := starlark.Entries(m)
	if m.itercount != 0 {
		t.Errorf("Entries(m) started an iteration: itercount = %d, want 0", m.itercount)
	}

	// Each iteration must be independent and must balance Iterate with Done.
	want := []string{`"one" 1`, `"two" 2`}
	for pass := range 2 {
		var got []string
		for k, v := range seq {
			got = append(got, fmt.Sprintf("%v %v", k, v))
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("pass %d: Entries(m) diff (-want +got):\n%s", pass, diff)
		}
		if m.itercount != 0 {
			t.Errorf("pass %d: unbalanced Iterate/Done: itercount = %d, want 0", pass, m.itercount)
		}
	}
}
