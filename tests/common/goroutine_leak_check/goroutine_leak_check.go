// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package goroutine_leak_check

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

var (
	funcNameRegex = regexp.MustCompile(`^(\S+)\(\S+`)
)

// CheckLeakedGoRoutines is used to check for goroutine leaks at the end of a test.
// It is not uncommon to leave a go routine that will never finish,
// e.g. blocked on a channel that is unreachable will never be closed.
// This function enumerates goroutines and reports any goroutines that are left running.
func CheckLeakedGoRoutines(funcWhitelist ...string) error {
	var leaked []string
	for tryIdx := 0; tryIdx < 8; tryIdx++ {
		// We need to explicitly call GC to call full GC procedures for real.
		// Otherwise, for example there is high probability of not calling
		// Finalizers (which are used in xcontext, for example).
		runtime.GC()
		runtime.Gosched()

		leaked = getLeakedGoroutines(funcWhitelist)
		if len(leaked) == 0 {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	sort.Strings(leaked)
	return fmt.Errorf("leaked goroutines:\n  %s\n", strings.Join(leaked, "\n  "))
}

func getLeakedGoroutines(funcWhitelist []string) []string {
	// Get goroutine stacks
	buf := make([]byte, 1000000)
	n := runtime.Stack(buf, true /* all */)
	strBuf := string(buf[:n])

	goroutinesStacks := strings.Split(strBuf, "\n\n")

	var result []string
	// First goroutine is always the running one, we skip it.
	for _, goroutineStack := range goroutinesStacks[1:] {
		stack := strings.Split(goroutineStack, "\n")
		// first line is goroutine info, skip it
		var whitelisted bool
		var userGoroutine bool
		for idx, line := range stack[1:] {
			if idx%2 == 1 {
				// Lines go in the following order:
				// testing.tRunner(0xc000117040, 0x125ec08)
				// /usr/local/go/src/testing/testing.go:1259 +0x1db
				// created by testing.(*T).Run
				// /usr/local/go/src/testing/testing.go:1306 +0x673
				// ...
				// Should skip path
				continue
			}
			m := funcNameRegex.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			funcName := m[1]

			// stdlib functions do not contain '/'
			if strings.Contains(funcName, "/") {
				userGoroutine = true
				for _, wle := range funcWhitelist {
					if matched, _ := path.Match(wle, funcName); matched {
						whitelisted = true
						break
					}
				}
			}
		}
		if userGoroutine && !whitelisted {
			result = append(result, goroutineStack)
		}
	}
	return result
}

func LeakCheckingTestMain(m *testing.M, funcWhitelist ...string) {
	ret := m.Run()
	if ret == 0 {
		if err := CheckLeakedGoRoutines(funcWhitelist...); err != nil {
			fmt.Fprintf(os.Stderr, "%s", err)
			ret = 1
		}
	}
	os.Exit(ret)
}
