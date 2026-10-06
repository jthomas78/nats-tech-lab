package main

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPlayground(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "demo 03 playground")
}

// The fixture readings are copies of exercise 10's kept run
// lab/run/evidence/10-20261005-170022/obs/ (ML51: the za thaw whose first
// polls disagree). The originals are gitignored and untouched; these copies
// are test data only, never evidence.
const fixtureDir = "testdata/obs-10-20261005-170022/"

// fixtureRounds loads a .jsonl file of classify-10.py readings, grouped by
// round, each round in server order.
func fixtureRounds(name string) [][]metaReading {
	f, err := os.Open(fixtureDir + name)
	Expect(err).NotTo(HaveOccurred())
	defer f.Close()

	by := map[int][]metaReading{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var line struct {
			metaReading
			Round int     `json:"round"`
			T     float64 `json:"t"`
		}
		Expect(json.Unmarshal(sc.Bytes(), &line)).To(Succeed())
		r := line.metaReading
		r.At = unixFloat(line.T)
		by[line.Round] = append(by[line.Round], r)
	}
	Expect(sc.Err()).NotTo(HaveOccurred())

	keys := make([]int, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	out := make([][]metaReading, len(keys))
	for i, k := range keys {
		out[i] = by[k]
	}
	return out
}

// fixtureThaw is ML51's thaw time, from ML51.thaw.
func fixtureThaw() time.Time {
	raw, err := os.ReadFile(fixtureDir + "ML51.thaw")
	Expect(err).NotTo(HaveOccurred())
	v, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
	Expect(err).NotTo(HaveOccurred())
	return unixFloat(v)
}

func unixFloat(v float64) time.Time {
	sec, frac := math.Modf(v)
	return time.Unix(int64(sec), int64(frac*1e9))
}

// storeOf turns one round into the monitor store's view. An unreachable
// reading is not an answer: the server keeps no answer (these fixtures carry
// none from before) and its newest attempt failed.
func storeOf(round []metaReading) []serverObs {
	out := make([]serverObs, 0, len(round))
	for i := range round {
		r := round[i]
		if r.Kind == kindUnreachable {
			out = append(out, serverObs{Server: r.Server, AttemptFailed: true})
			continue
		}
		out = append(out, serverObs{Server: r.Server, Answer: &r})
	}
	return out
}

// roundTime is the newest reading in a round: when the poll finished.
func roundTime(round []metaReading) time.Time {
	var t time.Time
	for _, r := range round {
		if r.At.After(t) {
			t = r.At
		}
	}
	return t
}
