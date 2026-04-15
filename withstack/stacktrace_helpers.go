// Copyright 2019 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package withstack

import (
	"errors"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors/errbase"
	pkgErr "github.com/pkg/errors"
)

// functionName splits a fully-qualified function name into package and function.
func functionName(fnName string) (pack string, name string) {
	name = fnName
	// We get this:
	//	runtime/debug.*T·ptrmethod
	// and want this:
	//  pack = runtime/debug
	//	name = *T.ptrmethod
	if idx := strings.LastIndex(name, "."); idx != -1 {
		pack = name[:idx]
		name = name[idx+1:]
	}
	name = strings.Replace(name, "·", ".", -1)
	return
}

// parsePrintedStackEntry extracts the stack entry information
// in lines at position i. It returns the new value of i if more than
// one line was read.
func parsePrintedStackEntry(
	lines []string, i int,
) (newI int, file string, line int, fnName string) {
	// The function name is on the first line.
	fnName = lines[i]

	// The file:line pair may be on the line after that.
	if i < len(lines)-1 && strings.HasPrefix(lines[i+1], "\t") {
		fileLine := strings.TrimSpace(lines[i+1])
		// Separate file path and line number.
		lineSep := strings.LastIndexByte(fileLine, ':')
		if lineSep == -1 {
			file = fileLine
		} else {
			file = fileLine[:lineSep]
			lineStr := fileLine[lineSep+1:]
			line, _ = strconv.Atoi(lineStr)
		}
		i++
	}
	return i, file, line, fnName
}

var pkgFundamental errbase.TypeKey
var pkgWithStackName errbase.TypeKey
var ourWithStackName errbase.TypeKey

func init() {
	err := errors.New("")
	pkgFundamental = errbase.GetTypeKey(pkgErr.New(""))
	pkgWithStackName = errbase.GetTypeKey(pkgErr.WithStack(err))
	ourWithStackName = errbase.GetTypeKey(WithStack(err))
}
