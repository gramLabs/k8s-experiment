/*
Copyright 2020 GramLabs, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"strings"
	"unicode"
)

// FixLatency returns a constant value from a user entered value.
func FixLatency(in LatencyType) LatencyType {
	switch strings.Map(func(r rune) rune {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, string(in)) {

	case "minimum", "min":
		return LatencyMinimum

	case "maximum", "max":
		return LatencyMaximum

	case "mean", "average", "avg":
		return LatencyMean

	case "percentile50", "p50", "median", "med":
		return LatencyPercentile50

	case "percentile95", "p95":
		return LatencyPercentile95

	case "percentile99", "p99":
		return LatencyPercentile99

	default:
		return ""
	}
}
