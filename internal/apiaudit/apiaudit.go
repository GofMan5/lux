// Package apiaudit sweeps public API surfaces for panics on zero values: it
// calls every exported method of each receiver, and each function, with
// zero-valued arguments and records what panics or hangs.
package apiaudit

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"time"
)

// Mode picks the arguments a sweep passes.
type Mode uint8

const (
	// Nil passes nil for every pointer, func, slice, map, chan and interface
	// except context.Context.
	Nil Mode = iota
	// Zero passes pointers to fresh zero values instead of nil pointers.
	Zero
)

// Hang is the per-call limit after which a call is reported as hung.
const Hang = 2 * time.Second

// Sweep calls every exported method of each receiver, or each value itself
// when it is a func, with arguments built by mode. Value methods are skipped
// on nil pointer receivers. It returns one
// "Type.Method: panic" line per call that panicked or hung, sorted.
func Sweep(mode Mode, values ...any) []string {
	var failures []string
	for _, value := range values {
		v := reflect.ValueOf(value)
		if v.Kind() == reflect.Func {
			failures = append(failures, call(funcName(v), v, mode)...)
			continue
		}
		nilPointer := v.Kind() == reflect.Pointer && v.IsNil()
		for i := 0; i < v.NumMethod(); i++ {
			method := v.Type().Method(i).Name
			name := fmt.Sprintf("%v.%s", v.Type(), method)
			if nilPointer {
				// A value method through a nil pointer panics in the language
				// itself, before the method runs; only pointer methods can guard.
				if _, value := v.Type().Elem().MethodByName(method); value {
					continue
				}
				name = fmt.Sprintf("(%v)(nil).%s", v.Type(), method)
			}
			failures = append(failures, call(name, v.Method(i), mode)...)
		}
	}
	sort.Strings(failures)
	return failures
}

func call(name string, fn reflect.Value, mode Mode) []string {
	t := fn.Type()
	args := make([]reflect.Value, t.NumIn())
	for i := range args {
		args[i] = argument(t.In(i), mode)
	}
	if t.IsVariadic() {
		args = args[:len(args)-1]
	}
	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		fn.Call(args)
	}()
	select {
	case recovered := <-done:
		if recovered != nil {
			return []string{fmt.Sprintf("%s: %v", name, recovered)}
		}
		return nil
	case <-time.After(Hang):
		return []string{name + ": hung"}
	}
}

var contextType = reflect.TypeOf((*context.Context)(nil)).Elem()

// argument never passes a nil context.Context: by Go convention it is not a
// valid input, so sweeps pass context.Background().
func argument(t reflect.Type, mode Mode) reflect.Value {
	if t == contextType {
		return reflect.ValueOf(context.Background())
	}
	if mode == Zero && t.Kind() == reflect.Pointer {
		return reflect.New(t.Elem())
	}
	return reflect.Zero(t)
}

func funcName(v reflect.Value) string {
	if fn := runtime.FuncForPC(v.Pointer()); fn != nil {
		return fn.Name()
	}
	return v.Type().String()
}
